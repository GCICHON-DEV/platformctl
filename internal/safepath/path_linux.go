//go:build linux

// Package safepath rejects symlinks component by component and holds directory
// descriptors through each operation. It does not sandbox executed commands.
package safepath

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// OpenDir anchors a directory without following any relative path symlink.
// The root is a trusted boundary chosen by the caller.
func OpenDir(root, path string, create bool) (*os.File, error) {
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("unsafe relative path %q", path)
	}
	fd, err := syscall.Open(root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), root)
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		next, err := syscall.Openat(int(current.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err == syscall.ENOENT && create {
			err = syscall.Mkdirat(int(current.Fd()), part, 0755)
			if err == nil || err == syscall.EEXIST {
				next, err = syscall.Openat(int(current.Fd()), part, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			}
		}
		current.Close()
		if err != nil {
			return nil, fmt.Errorf("open directory %s: %w", path, err)
		}
		current = os.NewFile(uintptr(next), part)
	}
	return current, nil
}

func parent(path string) (*os.File, string, error) {
	dir, err := OpenDir(".", filepath.Dir(path), false)
	if err != nil {
		return nil, "", err
	}
	return dir, filepath.Join(fmt.Sprintf("/proc/self/fd/%d", dir.Fd()), filepath.Base(path)), nil
}

// Rename pins both parents; rename replaces a final symlink rather than following it.
func Rename(from, to string, beforeRename func(string, string) error) error {
	a, source, err := parent(from)
	if err != nil {
		return err
	}
	defer a.Close()
	b, target, err := parent(to)
	if err != nil {
		return err
	}
	defer b.Close()
	if beforeRename != nil {
		if err := beforeRename(from, to); err != nil {
			return err
		}
	}
	return os.Rename(source, target)
}

func Remove(path string) error {
	dir, target, err := parent(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return os.Remove(target)
}

func ReadFile(root, path string) ([]byte, error) {
	dir, err := OpenDir(root, filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := syscall.Openat(int(dir.Fd()), filepath.Base(path), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source %s must be a regular file without symlinks", path)
	}
	return io.ReadAll(file)
}

// WriteFile is used for private staging files, never to truncate a destination.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	dir, err := OpenDir(".", filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer dir.Close()
	fd, err := syscall.Openat(int(dir.Fd()), filepath.Base(path), syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

func Chmod(path string, mode os.FileMode) error {
	dir, err := OpenDir(".", filepath.Dir(path), false)
	if err != nil {
		return err
	}
	defer dir.Close()
	// O_PATH pins even a mode-000 staging file. Reject a symlink before
	// chmod through proc; fchmod itself does not support O_PATH descriptors.
	const oPath = 0x200000
	fd, err := syscall.Openat(int(dir.Fd()), filepath.Base(path), oPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe staging file %s", path)
	}
	return os.Chmod(fmt.Sprintf("/proc/self/fd/%d", file.Fd()), mode)
}
