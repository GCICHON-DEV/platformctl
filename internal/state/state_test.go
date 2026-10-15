package state

import (
	"fmt"
	"os"
	"testing"
)

func TestSaveLoadAndResetPhase(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	st := &State{
		TemplateSource:   "./template",
		TemplateChecksum: "abc",
		CompletedSteps:   map[string]bool{"apply:01:test": true},
	}
	if err := Save(st); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.ManagedFiles == nil {
		t.Fatalf("managed files should be initialized")
	}
	if !loaded.CompletedSteps["apply:01:test"] {
		t.Fatalf("completed step was not persisted")
	}
	ResetPhase(loaded, "destroy")
	if loaded.LastPhase != "destroy" {
		t.Fatalf("last phase = %q, want destroy", loaded.LastPhase)
	}
	if len(loaded.CompletedSteps) != 0 {
		t.Fatalf("completed steps were not reset")
	}
}

func TestAcquireLockReclaimsStaleLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(DirName, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(), []byte("operation=apply pid=999999 started_at=2000-01-01T00:00:00Z\n"), 0644); err != nil {
		t.Fatal(err)
	}

	lock, err := AcquireLock("destroy")
	if err != nil {
		t.Fatalf("AcquireLock returned error: %v", err)
	}
	defer lock.Release()

	content, err := os.ReadFile(LockPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(content) == "operation=apply pid=999999 started_at=2000-01-01T00:00:00Z\n" {
		t.Fatalf("stale lock was not replaced")
	}
}

func TestAcquireLockPreventsConcurrentWorkflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	lock, err := AcquireLock("apply")
	if err != nil {
		t.Fatalf("AcquireLock returned error: %v", err)
	}
	if _, err := os.Stat(LockPath()); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}
	if _, err := AcquireLock("destroy"); err == nil {
		t.Fatal("second AcquireLock succeeded, want error")
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	next, err := AcquireLock("plan")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Release()
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock("apply"); err == nil {
		t.Fatal("old Release unlocked new owner")
	}
	if _, err := os.Stat(LockPath()); err != nil {
		t.Fatalf("stable lock inode missing: %v", err)
	}
}

func TestLegacyLockWithLivePIDFailsClosed(t *testing.T) {
	inWorkspace(t)
	if err := os.MkdirAll(DirName, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("operation=apply pid=%d started_at=2000-01-01T00:00:00Z\n", os.Getpid())
	if err := os.WriteFile(LockPath(), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock("plan"); err == nil {
		t.Fatal("live legacy PID (including PID reuse) must fail closed")
	}
	got, _ := os.ReadFile(LockPath())
	if string(got) != content {
		t.Fatal("legacy lock was changed")
	}
}

func TestKernelLockIgnoresStalePIDMetadata(t *testing.T) {
	inWorkspace(t)
	if err := os.MkdirAll(DirName, 0755); err != nil {
		t.Fatal(err)
	}
	// A stale v2 file may name a PID that has since been reused by this process.
	content := fmt.Sprintf("%soperation=apply pid=%d started_at=2000-01-01T00:00:00Z\n", lockFormat, os.Getpid())
	if err := os.WriteFile(LockPath(), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireLock("plan")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
}

func TestReleaseDoesNotRemoveReplacedPath(t *testing.T) {
	inWorkspace(t)
	lock, err := AcquireLock("apply")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	// Manual unlink/replacement is unsupported, but Release must never delete it.
	if err := os.Rename(LockPath(), LockPath()+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(), []byte("other owner"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(LockPath())
	if err != nil || string(data) != "other owner" {
		t.Fatalf("replacement changed: %s %v", data, err)
	}
}

func inWorkspace(t *testing.T) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedLegacyAndSymlinkLockFailClosed(t *testing.T) {
	inWorkspace(t)
	if err := os.MkdirAll(DirName, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(), []byte("unknown lock owner"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock("plan"); err == nil {
		t.Fatal("malformed legacy lock accepted")
	}
	if err := os.Rename(LockPath(), LockPath()+".target"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("lock.target", LockPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock("plan"); err == nil {
		t.Fatal("symlink lock accepted")
	}
	data, err := os.ReadFile(LockPath() + ".target")
	if err != nil || string(data) != "unknown lock owner" {
		t.Fatalf("lock target changed: %s %v", data, err)
	}
}
