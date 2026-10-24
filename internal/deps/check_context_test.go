package deps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVersionCheckFailureIsRequired(t *testing.T) {
	bin := t.TempDir()
	for _, tc := range []struct{ name, body string }{
		{"timeout", "/bin/sleep 30"},
		{"exit", "exit 7"},
		{"output", "while :; do printf '01234567890123456789012345678901234567890123456789'; done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(bin, tc.name)
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.body), 0700); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			status := CheckOneContext(context.Background(), 100*time.Millisecond, Tool{Name: tc.name, PathEnv: bin, VersionArgs: []string{"--version"}})
			if !status.Installed || status.CheckError == "" || !strings.Contains(VersionProblem(status), "check failed") || time.Since(start) > 2*time.Second {
				t.Fatalf("%+v elapsed=%s", status, time.Since(start))
			}
		})
	}
	if VersionProblem(Status{Installed: true, Tool: Tool{Name: "pinned", Version: "1.0.0"}}) == "" {
		t.Fatal("empty pinned version accepted")
	}
}
