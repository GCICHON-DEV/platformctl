package executor

import (
	"bytes"
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
