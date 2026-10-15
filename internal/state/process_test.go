package state

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStateProcess(t *testing.T) {
	mode := os.Getenv("PLATFORMCTL_STATE_PROCESS")
	if mode == "" {
		return
	}
	if mode == "hold" {
		lock, err := AcquireLock("holder")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer lock.Release()
		if err := os.WriteFile("ready", nil, 0600); err != nil {
			os.Exit(1)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	for i := 0; i < 12; i++ {
		deadline := time.Now().Add(10 * time.Second)
		var lock *Lock
		for {
			var err error
			lock, err = AcquireLock("increment")
			if err == nil {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			time.Sleep(time.Millisecond)
		}
		st, err := Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		count, _ := strconv.Atoi(st.Metadata["counter"])
		st.Metadata["counter"] = strconv.Itoa(count + 1)
		st.CompletedSteps[fmt.Sprintf("%s:%d", mode, i)] = true
		if err := Save(st); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := lock.Release(); err != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}

func stateProcess(root, mode string) *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^TestStateProcess$")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PLATFORMCTL_STATE_PROCESS="+mode)
	return cmd
}

func TestProcessesReclaimCrashAndPreserveEveryCheckpoint(t *testing.T) {
	root := t.TempDir()
	holder := stateProcess(root, "hold")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("holder not ready")
		}
		time.Sleep(time.Millisecond)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait()
	// Exercise simultaneous reclamation of a legacy lock whose actual owner has
	// crashed and been reaped. The kernel lock inode must not be replaced.
	path := filepath.Join(root, LockPath())
	if err := os.WriteFile(path, []byte(fmt.Sprintf("operation=apply pid=%d started_at=2000-01-01T00:00:00Z\n", holder.Process.Pid)), 0644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	workers := make([]*exec.Cmd, 6)
	for i := range workers {
		workers[i] = stateProcess(root, strconv.Itoa(i))
		if err := workers[i].Start(); err != nil {
			t.Fatal(err)
		}
		cmd := workers[i]
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	}
	finished := make(chan error, 1)
	go func() {
		var first error
		for _, cmd := range workers {
			if err := cmd.Wait(); err != nil && first == nil {
				first = err
			}
		}
		finished <- first
	}()
	for {
		// Readers must see an entire JSON checkpoint even during competing writes.
		data, err := os.ReadFile(filepath.Join(root, Path()))
		if err == nil {
			var st State
			if err := json.Unmarshal(data, &st); err != nil {
				t.Fatalf("torn state: %s: %v", data, err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, Path()))
			if err != nil {
				t.Fatal(err)
			}
			var st State
			if err := json.Unmarshal(data, &st); err != nil {
				t.Fatal(err)
			}
			if st.Metadata["counter"] != "72" || len(st.CompletedSteps) != 72 {
				t.Fatalf("lost checkpoint: counter=%s checkpoints=%d", st.Metadata["counter"], len(st.CompletedSteps))
			}
			after, err := os.Stat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("lock inode was replaced: %v", err)
			}
			return
		default:
			time.Sleep(time.Millisecond)
		}
	}
}
