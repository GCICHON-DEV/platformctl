//go:build darwin

package executor

import (
	"errors"
	"os"
	"os/exec"
	"runtime"

	"golang.org/x/sys/unix"
)

func runPinned(cmd *exec.Cmd, cwd *os.File) error {
	// Darwin /dev/fd directory entries cannot be traversed like Linux /proc.
	// A thread-local cwd gives the child a pinned directory without changing
	// the process cwd observed by other goroutines. Never use process chdir.
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.PthreadFchdir(int(cwd.Fd())); err != nil {
			runtime.UnlockOSThread()
			done <- err
			return
		}
		runErr := cmd.Run()
		resetErr := unix.PthreadFchdir(-1)
		if resetErr == nil {
			runtime.UnlockOSThread()
		}
		// If reset fails, exiting while locked makes Go discard this OS thread.
		done <- errors.Join(runErr, resetErr)
	}()
	return <-done
}
