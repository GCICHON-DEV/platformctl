//go:build darwin || linux

package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
)

func privateOwned(info os.FileInfo, directory bool) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || info.Mode().Perm()&0077 != 0 {
		return false
	}
	if directory {
		return info.IsDir()
	}
	return info.Mode().IsRegular() && stat.Nlink == 1
}

func privateFileHash(path string) (string, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !privateOwned(info, false) {
		return "", fmt.Errorf("bootstrap file changed ownership/type")
	}
	data, err := io.ReadAll(io.LimitReader(file, 128*1024+1))
	if err != nil || len(data) > 128*1024 {
		return "", fmt.Errorf("bootstrap kubeconfig exceeds limit or cannot be read")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
