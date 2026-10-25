//go:build !darwin && !linux

package templateengine

import "os"

func openTargetFile(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	return os.Open(path)
}
