package checkexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBoundedCheck(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, script string
		want         error
	}{
		{"timeout", "sleep 30", context.DeadlineExceeded},
		{"output", "while :; do printf '01234567890123456789012345678901234567890123456789'; done", ErrOutputLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			out, err := Run(context.Background(), 150*time.Millisecond, sh, []string{"-c", tc.script}, os.Environ())
			if !errors.Is(err, tc.want) || len(out) != 0 || time.Since(start) > 2*time.Second {
				t.Fatalf("out=%d err=%v elapsed=%s", len(out), err, time.Since(start))
			}
		})
	}
	out, err := Run(context.Background(), time.Second, sh, []string{"-c", "printf ok; printf err >&2"}, os.Environ())
	if err != nil || !strings.Contains(string(out), "ok") || !strings.Contains(string(out), "err") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestCheckParentExitWithDescendantPipe(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = Run(context.Background(), 300*time.Millisecond, sh, []string{"-c", "sleep 30 & exit 0"}, os.Environ())
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(start))
	}
}

func TestTimeoutStopsDescendantWork(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "survived")
	_, err = Run(context.Background(), 100*time.Millisecond, sh, []string{"-c", `(/bin/sleep 0.4; printf survived > "$1") & wait`, "check", sentinel}, os.Environ())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("descendant survived timeout: %v", err)
	}
}
