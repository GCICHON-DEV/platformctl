//go:build !linux && !darwin

package executor

import (
	"fmt"
	"os"
	"os/exec"
)

func runPinned(cmd *exec.Cmd, cwd *os.File) error {
	return fmt.Errorf("secure workflow cwd requires Linux or macOS")
}
