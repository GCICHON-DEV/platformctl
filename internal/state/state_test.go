package state

import (
	"errors"
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
	if _, err := os.Stat(LockPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock file still exists: %v", err)
	}
}
