//go:build unix

package templateengine

import (
	"os"
	"syscall"
	"testing"
)

func TestTransactionRespectsUmaskForNewFiles(t *testing.T) {
	chdirForTest(t, t.TempDir())
	// An already accessible parent must not expose a newly rendered secret.
	if err := os.Mkdir("generated", 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, "generated/existing.txt", "old")
	if err := os.Chmod("generated/existing.txt", 0640); err != nil {
		t.Fatal(err)
	}
	previousMask := syscall.Umask(0077)
	defer syscall.Umask(previousMask)
	p := transactionPlan(map[string]string{
		"generated/new.txt": "private", "generated/existing.txt": "updated",
	})
	if _, err := p.GenerateWithPrevious([]string{"generated/existing.txt"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		"generated/new.txt": 0600, "generated/existing.txt": 0640,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s permissions = %04o, want %04o", path, got, want)
		}
	}
}
