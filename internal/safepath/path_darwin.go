//go:build darwin

// Package safepath rejects symlinks component by component and holds directory
// descriptors through each operation. It does not sandbox executed commands.
package safepath

import (
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// OpenDir anchors a directory without following any relative path symlink.
// The root is a trusted boundary chosen by the caller.
func OpenDir(root, path string, create bool) (*os.File, error) {
	if filepath.IsAbs(path) || path == ".." || strings.HasPrefix(filepath.Clean(path), ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("unsafe relative path %q", path)
	}
	fd, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), root)
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "." {
			continue
		}
		next, err := unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err == unix.ENOENT && create {
			err = unix.Mkdirat(int(current.Fd()), part, 0755)
			if err == nil || err == unix.EEXIST {
				next, err = unix.Openat(int(current.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
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
	return dir, filepath.Base(path), nil
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
	return unix.Renameat(int(a.Fd()), source, int(b.Fd()), target)
}

func Remove(path string) error {
	dir, target, err := parent(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	var info unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), target, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	flags := 0
	if info.Mode&unix.S_IFMT == unix.S_IFDIR {
		flags = unix.AT_REMOVEDIR
	}
	return unix.Unlinkat(int(dir.Fd()), target, flags)
}

func ReadFile(root, path string) ([]byte, error) {
	dir, err := OpenDir(root, filepath.Dir(path), false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
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
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(mode.Perm()))
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
	// fchmodat with NOFOLLOW does not require opening a mode-000 file.
	// The final component is never dereferenced, even if replaced after stat.
	leaf := filepath.Base(path)
	var info unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), leaf, &info, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("unsafe staging file %s", path)
	}
	return unix.Fchmodat(int(dir.Fd()), leaf, uint32(mode.Perm()), unix.AT_SYMLINK_NOFOLLOW)
}
