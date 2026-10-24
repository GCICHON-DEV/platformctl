package executor

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunnerUsesManagedPathBeforeSystemPath(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "managed-tool")
	if runtime.GOOS == "windows" {
		tool += ".exe"
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\necho managed\n"), 0755); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	runner := NewRunner(&out, &out).WithPathEnv(dir + string(os.PathListSeparator) + os.Getenv("PATH"))
	if err := runner.RunStep("test", "", "managed-tool"); err != nil {
		t.Fatalf("RunStep returned error: %v\n%s", err, out.String())
	}
	if !bytes.Contains(out.Bytes(), []byte("managed")) {
		t.Fatalf("runner did not execute managed tool: %s", out.String())
	}
}

func TestMaskCommandRedactsSensitiveAssignments(t *testing.T) {
	got := MaskCommand("terraform", []string{"apply", "token=abc123", "password:secret", "plain=value"})
	if bytes.Contains([]byte(got), []byte("abc123")) || bytes.Contains([]byte(got), []byte("secret")) {
		t.Fatalf("MaskCommand leaked sensitive values: %s", got)
	}
	if !bytes.Contains([]byte(got), []byte("plain=value")) {
		t.Fatalf("MaskCommand redacted non-sensitive value: %s", got)
	}
}

func TestRunnerRejectsSymlinkCwd(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	outside := t.TempDir()
	if err := os.Symlink(outside, "generated"); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(io.Discard, io.Discard)
	if err := runner.RunStep("unsafe", "generated", "sh", "-c", "touch marker"); err == nil {
		t.Fatal("followed cwd symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "marker")); !os.IsNotExist(err) {
		t.Fatalf("outside touched: %v", err)
	}
}

type swapCwdWriter struct {
	swap func()
	done bool
}

func (w *swapCwdWriter) Write(p []byte) (int, error) {
	if !w.done {
		w.done = true
		w.swap()
	}
	return len(p), nil
}

func TestRunnerPinsCwdBeforeConcurrentSwap(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	outside := t.TempDir()
	if err := os.Mkdir("generated", 0700); err != nil {
		t.Fatal(err)
	}
	output := &swapCwdWriter{swap: func() {
		if err := os.Rename("generated", "original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, "generated"); err != nil {
			t.Fatal(err)
		}
	}}
	if err := NewRunner(output, io.Discard).RunStep("pinned", "generated", "sh", "-c", "printf safe > marker"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "marker")); !os.IsNotExist(err) {
		t.Fatalf("outside touched: %v", err)
	}
	if data, err := os.ReadFile("original/marker"); err != nil || string(data) != "safe" {
		t.Fatalf("cwd not pinned: %q %v", data, err)
	}
}
