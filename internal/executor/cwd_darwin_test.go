//go:build darwin

package executor

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedDarwinCwdIsThreadLocal(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	cwd, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer cwd.Close()
	var out bytes.Buffer
	command := exec.Command("/bin/sh", "-c", "pwd; sleep 0.1")
	command.Stdout = &out
	done := make(chan error, 1)
	go func() { done <- runPinned(command, cwd) }()
	for i := 0; i < 1000; i++ {
		observed, err := os.Getwd()
		if err != nil || observed != before {
			t.Fatalf("process cwd changed: %q %v", observed, err)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	realTarget, _ := filepath.EvalSymlinks(target)
	if got := strings.TrimSpace(out.String()); got != realTarget {
		t.Fatalf("child cwd %q, want %q", got, realTarget)
	}
	if observed, _ := os.Getwd(); observed != before {
		t.Fatalf("cwd leaked: %q", observed)
	}
}
