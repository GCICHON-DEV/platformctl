//go:build linux

package executor

import (
	"fmt"
	"os"
	"os/exec"
)

func runPinned(cmd *exec.Cmd, cwd *os.File) error {
	// Go chdirs before remapping ExtraFiles and close-on-exec.
	cmd.Dir = fmt.Sprintf("/proc/self/fd/%d", cwd.Fd())
	return cmd.Run()
}
