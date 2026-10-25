//go:build darwin || linux

package templateengine

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadTargetFileRefusesSpecialAndOversize(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTargetFile(fifo, 32); err == nil {
		t.Fatal("FIFO accepted")
	}
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTargetFile(regular, 3); err == nil {
		t.Fatal("oversized file accepted")
	}
	if data, err := ReadTargetFile(regular, 4); err != nil || string(data) != "1234" {
		t.Fatalf("valid file: %q %v", data, err)
	}
}
