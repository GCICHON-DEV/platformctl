package templateengine

import (
	"fmt"
	"io"
)

// ReadTargetFile bounds identity configuration reads and refuses special files.
func ReadTargetFile(path string, limit int64) ([]byte, error) {
	file, err := openTargetFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("target configuration must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("target configuration exceeds byte limit")
	}
	return data, nil
}
