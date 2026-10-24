//go:build !linux && !darwin

package safepath

import (
	"fmt"
	"os"
)

func unsupported() error {
	return fmt.Errorf("secure workspace operations require Linux or macOS directory descriptors")
}
func OpenDir(root, path string, create bool) (*os.File, error)        { return nil, unsupported() }
func Rename(from, to string, rename func(string, string) error) error { return unsupported() }
func Remove(path string) error                                        { return unsupported() }
func ReadFile(root, path string) ([]byte, error)                      { return nil, unsupported() }
func WriteFile(path string, data []byte, mode os.FileMode) error      { return unsupported() }
func Chmod(path string, mode os.FileMode) error                       { return unsupported() }
