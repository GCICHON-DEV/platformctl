package templateengine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func transactionPlan(files map[string]string) *ExecutionPlan {
	p := &ExecutionPlan{contents: files, FileDigests: map[string]string{}}
	for path, content := range files {
		p.GeneratedFiles = append(p.GeneratedFiles, path)
		p.FileDigests[path] = checksum([]byte(content))
	}
	// Generation orders the union of previous and desired paths itself.
	return p
}

func transactionSnapshot(t *testing.T) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result[path] = info.Mode().String()
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			result[path] += ":" + target
		} else if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			result[path] += ":" + string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func transactionFixture(t *testing.T) (*ExecutionPlan, []string) {
	t.Helper()
	chdirForTest(t, t.TempDir())
	writeFile(t, "generated/b.txt", "old-b")
	writeFile(t, "generated/c-stale.txt", "old-c")
	writeFile(t, "generated/d.txt", "old-d")
	writeFile(t, "generated/manual.txt", "manual")
	writeFile(t, ".platformctl/state.json", "state must stay unchanged")
	if err := os.Chmod("generated/b.txt", 0600); err != nil {
		t.Fatal(err)
	}
	return transactionPlan(map[string]string{
		"generated/a/new.txt": "new-a", "generated/b.txt": "new-b", "generated/d.txt": "new-d",
	}), []string{"generated/b.txt", "generated/c-stale.txt", "generated/d.txt"}
}

func TestTransactionRollsBackEveryPublishFailure(t *testing.T) {
	for failAt := 1; failAt <= 6; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			p, previous := transactionFixture(t)
			before := transactionSnapshot(t)
			calls := 0
			injected := &os.PathError{Op: "rename", Path: "injected", Err: os.ErrPermission}
			_, err := p.generateTransaction(previous, transactionIO{os.WriteFile, func(from, to string) error {
				calls++
				if calls == failAt {
					return injected
				}
				return nil
			}})
			if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "rolled back") {
				t.Fatalf("expected rolled back permission failure, got %v", err)
			}
			if after := transactionSnapshot(t); !reflect.DeepEqual(before, after) {
				t.Fatalf("filesystem changed after rollback:\nbefore %v\nafter %v", before, after)
			}
		})
	}
}

func TestTransactionStagesAllFilesBeforePublishing(t *testing.T) {
	for failAt := 1; failAt <= 3; failAt++ {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			p, previous := transactionFixture(t)
			before := transactionSnapshot(t)
			calls := 0
			_, err := p.generateTransaction(previous, transactionIO{func(path string, data []byte, mode os.FileMode) error {
				calls++
				if calls == failAt {
					// Model a partial write before disk failure.
					if err := os.WriteFile(path, []byte("partial"), mode); err != nil {
						return err
					}
					return errors.New("injected write failure")
				}
				return os.WriteFile(path, data, mode)
			}, func(string, string) error {
				t.Fatal("published before all staging writes succeeded")
				return nil
			}})
			if err == nil || !strings.Contains(err.Error(), "injected write failure") {
				t.Fatalf("expected write failure, got %v", err)
			}
			if !reflect.DeepEqual(before, transactionSnapshot(t)) {
				t.Fatal("staging failure changed files")
			}
		})
	}
}

func TestTransactionSuccess(t *testing.T) {
	p, previous := transactionFixture(t)
	if _, err := p.GenerateWithPrevious(previous); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"generated/a/new.txt": "new-a", "generated/b.txt": "new-b", "generated/d.txt": "new-d",
		"generated/manual.txt": "manual", ".platformctl/state.json": "state must stay unchanged",
	} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v", path, data, err)
		}
	}
	if _, err := os.Stat("generated/c-stale.txt"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale file still exists: %v", err)
	}
	info, err := os.Stat("generated/b.txt")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("managed permissions changed: %v, %v", info, err)
	}
	stages, _ := filepath.Glob(".platformctl-render-*")
	if len(stages) != 0 {
		t.Fatalf("staging not cleaned: %v", stages)
	}
}

func TestTransactionRejectsCollisionEvenWithIdenticalBytes(t *testing.T) {
	chdirForTest(t, t.TempDir())
	writeFile(t, "generated/manual.txt", "same")
	writeFile(t, "generated/stale.txt", "old")
	before := transactionSnapshot(t)
	p := transactionPlan(map[string]string{"generated/manual.txt": "same"})
	_, err := p.GenerateWithPrevious([]string{"generated/stale.txt"})
	if err == nil || !strings.Contains(err.Error(), "unmanaged file collision") {
		t.Fatalf("expected explicit collision decision, got %v", err)
	}
	if !reflect.DeepEqual(before, transactionSnapshot(t)) {
		t.Fatal("collision changed files")
	}
}

func TestRenderFailurePreservesPreviousFiles(t *testing.T) {
	chdirForTest(t, t.TempDir())
	writeFile(t, "generated/old.txt", "old")
	writeFile(t, "generated/manual.txt", "manual")
	before := transactionSnapshot(t)
	r := &Resolved{Values: map[string]interface{}{}, Manifest: Manifest{Files: []GeneratedFile{
		{Path: "generated/new.txt", Content: "valid"},
		{Path: "generated/broken.txt", Content: "{{ .Values.missing }}"},
	}}}
	if _, err := r.GenerateWithPrevious([]string{"generated/old.txt"}); err == nil {
		t.Fatal("expected render failure")
	}
	if !reflect.DeepEqual(before, transactionSnapshot(t)) {
		t.Fatal("render failure changed files")
	}
}

func TestTransactionRejectsUnsafePathsBeforeMutation(t *testing.T) {
	for _, kind := range []string{"symlink-parent", "symlink-file", "stale-symlink", "directory", "overlap", "invalid-previous", "missing-content"} {
		t.Run(kind, func(t *testing.T) {
			chdirForTest(t, t.TempDir())
			writeFile(t, "generated/old.txt", "old")
			writeFile(t, "outside/data.txt", "private")
			previous := []string{"generated/old.txt"}
			p := transactionPlan(map[string]string{"generated/new.txt": "new"})
			switch kind {
			case "symlink-parent":
				if err := os.Symlink("../outside", "generated/link"); err != nil {
					t.Fatal(err)
				}
				p = transactionPlan(map[string]string{"generated/link/data.txt": "new"})
			case "symlink-file", "stale-symlink":
				if err := os.Symlink("../outside/data.txt", "generated/link"); err != nil {
					t.Fatal(err)
				}
				previous = append(previous, "generated/link")
				if kind == "symlink-file" {
					p = transactionPlan(map[string]string{"generated/link": "new"})
				}
			case "directory":
				if err := os.Mkdir("generated/new.txt", 0755); err != nil {
					t.Fatal(err)
				}
			case "overlap":
				p = transactionPlan(map[string]string{"generated/old.txt/child": "new"})
			case "invalid-previous":
				previous = append(previous, "outside/data.txt")
			case "missing-content":
				delete(p.contents, "generated/new.txt")
			}
			before := transactionSnapshot(t)
			if _, err := p.GenerateWithPrevious(previous); err == nil {
				t.Fatal("expected unsafe path or invalid content error")
			}
			if !reflect.DeepEqual(before, transactionSnapshot(t)) {
				t.Fatal("validation failure changed files")
			}
		})
	}
}

func TestTransactionRetainsBackupsWhenRollbackFails(t *testing.T) {
	chdirForTest(t, t.TempDir())
	writeFile(t, "generated/out.txt", "original")
	p := transactionPlan(map[string]string{"generated/out.txt": "new"})
	calls := 0
	_, err := p.generateTransaction([]string{"generated/out.txt"}, transactionIO{os.WriteFile, func(from, to string) error {
		calls++
		if calls > 1 {
			return os.ErrPermission
		} // Fail publication and restoration.
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("expected explicit recovery error, got %v", err)
	}
	backups, _ := filepath.Glob(".platformctl-render-*/old-0")
	if len(backups) != 1 || !strings.Contains(err.Error(), filepath.Dir(backups[0])) {
		t.Fatalf("backup path absent from error: %v, %v", backups, err)
	}
	data, readErr := os.ReadFile(backups[0])
	if readErr != nil || string(data) != "original" {
		t.Fatalf("original backup lost: %q, %v", data, readErr)
	}
}

func TestTransactionRejectsParentSwapAfterStaging(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			chdirForTest(t, t.TempDir())
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "file"), "private")
			writeFile(t, "generated/file", "old")
			p := transactionPlan(map[string]string{"generated/file": "new"})
			if stale {
				p = transactionPlan(map[string]string{"generated/new": "new"})
			}
			swapped := false
			_, err := p.generateTransaction([]string{"generated/file"}, transactionIO{func(path string, data []byte, mode os.FileMode) error {
				if !swapped {
					if err := os.Rename("generated", "original"); err != nil {
						return err
					}
					if err := os.Symlink(outside, "generated"); err != nil {
						return err
					}
					swapped = true
				}
				return os.WriteFile(path, data, mode)
			}, nil})
			if err == nil {
				t.Fatal("expected unsafe parent rejection")
			}
			data, err := os.ReadFile(filepath.Join(outside, "file"))
			if err != nil || string(data) != "private" {
				t.Fatalf("outside changed: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(outside, "new")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("outside file created: %v", err)
			}
		})
	}
}
