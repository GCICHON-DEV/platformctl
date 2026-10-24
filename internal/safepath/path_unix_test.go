//go:build linux || darwin

package safepath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRejectsSymlinks(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ name, target, path string }{{"parent", outside, "parent/secret"}, {"leaf", filepath.Join(outside, "secret"), "leaf"}} {
		if err := os.Symlink(item.target, filepath.Join(root, item.name)); err != nil {
			t.Fatal(err)
		}
		if data, err := ReadFile(root, item.path); err == nil {
			t.Fatalf("followed link: %q", data)
		}
	}
}

func TestRenamePinsParentsDuringOperation(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := os.Mkdir("generated", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("staged", []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "file"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	err = Rename("staged", "generated/file", func(from, to string) error {
		if err := os.Rename("generated", "original"); err != nil {
			return err
		}
		if err := os.Symlink(outside, "generated"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(outside, "file"))
	if err != nil || string(data) != "private" {
		t.Fatalf("outside changed: %q %v", data, err)
	}
	data, err = os.ReadFile("original/file")
	if err != nil || string(data) != "new" {
		t.Fatalf("pinned write failed: %q %v", data, err)
	}
	if err := Remove("generated/file"); err == nil {
		t.Fatal("removal followed parent symlink")
	}
}

func TestReadDuringLeafReplacement(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "source")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 200; i++ {
			temp := filepath.Join(root, "replacement")
			if err := os.Symlink(secret, temp); err != nil {
				done <- err
				return
			}
			if err := os.Rename(temp, target); err != nil {
				done <- err
				return
			}
			if err := os.WriteFile(temp, []byte("safe"), 0600); err != nil {
				done <- err
				return
			}
			if err := os.Rename(temp, target); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 400; i++ {
		data, err := ReadFile(root, "source")
		if err == nil && string(data) != "safe" {
			t.Errorf("read outside source: %q", data)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRenameDoesNotFollowReplacedLeaf(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("staged", []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	err = Rename("staged", "target", func(from, to string) error {
		if err := os.Symlink(secret, "target"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(secret); err != nil || string(data) != "private" {
		t.Fatalf("outside changed: %q %v", data, err)
	}
}

func TestChmodPinsUnreadableStagingFile(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if err := WriteFile("staged", []byte("private"), 0000); err != nil {
		t.Fatal(err)
	}
	if err := Chmod("staged", 0000); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("staged", "link"); err != nil {
		t.Fatal(err)
	}
	if err := Chmod("link", 0644); err == nil {
		t.Fatal("chmod followed symlink")
	}
	info, err := os.Stat("staged")
	if err != nil || info.Mode().Perm() != 0000 {
		t.Fatalf("permissions changed: %v %v", info, err)
	}
}
