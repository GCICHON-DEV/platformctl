//go:build !darwin && !linux

package cmd

import (
	"fmt"
	"os"
)

func privateOwned(info os.FileInfo, directory bool) bool { return false }
func privateFileHash(path string) (string, error) {
	return "", fmt.Errorf("private bootstrap is supported on macOS and Linux")
}
