//go:build darwin || linux

package templateengine

import (
	"os"
	"syscall"
)

func openTargetFile(path string) (*os.File, error) {
	// Nonblocking open prevents FIFO/device replacement from hanging before fstat.
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
