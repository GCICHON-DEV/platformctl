package templateengine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"platformctl/internal/safepath"
)

// Per-call operations let tests inject filesystem failures without global hooks.
type transactionIO struct {
	writeFile func(string, []byte, os.FileMode) error
	rename    func(string, string) error
}

type fileChange struct {
	path, staged, backup string
	existed, installed   bool
	mode                 os.FileMode
}

func (p *ExecutionPlan) generateTransaction(previous []string, fs transactionIO) ([]string, error) {
	managed := map[string]bool{}
	for _, path := range previous {
		if err := validateGeneratedPath(path); err != nil {
			return nil, err
		}
		managed[filepath.Clean(path)] = true
	}
	desired := map[string]bool{}
	for _, path := range p.GeneratedFiles {
		if err := validateGeneratedPath(path); err != nil {
			return nil, err
		}
		if path != filepath.Clean(path) || desired[path] {
			return nil, fmt.Errorf("duplicate or non-canonical generated path %q", path)
		}
		content, ok := p.contents[path]
		if !ok || checksum([]byte(content)) != p.FileDigests[path] {
			return nil, fmt.Errorf("missing or changed captured content for %s", path)
		}
		desired[path] = true
	}
	paths := map[string]bool{}
	for path := range managed {
		paths[path] = true
	}
	for path := range desired {
		paths[path] = true
	}
	ordered := make([]string, 0, len(paths))
	for path := range paths {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	changes := make([]fileChange, 0, len(ordered))
	for _, path := range ordered {
		// A file-to-directory transition needs an explicit separate migration.
		for dir := filepath.Dir(path); dir != "."; dir = filepath.Dir(dir) {
			if paths[dir] {
				return nil, fmt.Errorf("overlapping managed file paths: %s and %s", dir, path)
			}
		}
		info, err := inspectGeneratedPath(path)
		if err != nil {
			return nil, err
		}
		if info != nil && desired[path] && !managed[path] {
			return nil, fmt.Errorf("unmanaged file collision at %s: move the file aside explicitly before retrying; no files changed", path)
		}
		change := fileChange{path: path, mode: 0644, existed: info != nil}
		if info != nil {
			change.mode = info.Mode().Perm()
		}
		changes = append(changes, change)
	}

	// Same workspace filesystem as the destinations; private directory also holds
	// original files until every replacement/removal has succeeded.
	stage, err := os.MkdirTemp(".", ".platformctl-render-")
	if err != nil {
		return nil, fmt.Errorf("create render staging: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	for i := range changes {
		change := &changes[i]
		if desired[change.path] {
			change.staged = filepath.Join(stage, fmt.Sprintf("new-%d", i))
			if err := fs.writeFile(change.staged, []byte(p.contents[change.path]), change.mode); err != nil {
				return nil, fmt.Errorf("stage %s: %w", change.path, err)
			}
			// Existing files retain their exact permissions. New files keep the
			// permissions selected by WriteFile and the process umask.
			if change.existed {
				if err := safepath.Chmod(change.staged, change.mode); err != nil {
					return nil, fmt.Errorf("stage permissions %s: %w", change.path, err)
				}
			}
		}
	}
	var createdDirs []string
	rollback := func(cause error) ([]string, error) {
		var failures []error
		for i := len(changes) - 1; i >= 0; i-- {
			change := &changes[i]
			if change.installed {
				if err := safepath.Remove(change.path); err != nil {
					failures = append(failures, fmt.Errorf("remove replacement %s: %w", change.path, err))
					continue
				}
			}
			if change.backup != "" {
				if err := safepath.Rename(change.backup, change.path, fs.rename); err != nil {
					failures = append(failures, fmt.Errorf("restore %s from %s: %w", change.path, change.backup, err))
				}
			}
		}
		for i := len(createdDirs) - 1; i >= 0; i-- {
			if err := safepath.Remove(createdDirs[i]); err != nil {
				failures = append(failures, fmt.Errorf("remove created directory %s: %w", createdDirs[i], err))
			}
		}
		if len(failures) != 0 {
			cleanup = false
			return nil, errors.Join(cause, fmt.Errorf("rollback incomplete; preserve recovery files in %s: %w", stage, errors.Join(failures...)))
		}
		return nil, fmt.Errorf("file transaction rolled back: %w", cause)
	}
	for i := range changes {
		change := &changes[i]
		if change.staged != "" {
			if err := makeGeneratedDirs(filepath.Dir(change.path), &createdDirs); err != nil {
				return rollback(err)
			}
		}
		if change.existed {
			backup := filepath.Join(stage, fmt.Sprintf("old-%d", i))
			if err := safepath.Rename(change.path, backup, fs.rename); err != nil {
				return rollback(fmt.Errorf("back up %s: %w", change.path, err))
			}
			change.backup = backup
		}
		if change.staged != "" {
			if err := safepath.Rename(change.staged, change.path, fs.rename); err != nil {
				return rollback(fmt.Errorf("publish %s: %w", change.path, err))
			}
			change.installed = true
		}
	}
	for _, change := range changes {
		if !desired[change.path] {
			pruneGeneratedDir(filepath.Dir(change.path))
		}
	}
	return append([]string{}, p.GeneratedFiles...), nil
}

// Reject symlinks in every component, directories at file targets and special
// files. Lstat prevents following links outside generated/, including stale paths.
func inspectGeneratedPath(path string) (os.FileInfo, error) {
	var result os.FileInfo
	for current := path; current != "."; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", current, err)
		}
		if (current == path && !info.Mode().IsRegular()) || (current != path && !info.IsDir()) {
			return nil, fmt.Errorf("unsafe generated path %s: expected regular files and directory parents, without symlinks", current)
		}
		if current == path {
			result = info
		}
	}
	return result, nil
}

func makeGeneratedDirs(dir string, created *[]string) error {
	if dir == "." {
		return nil
	}
	handle, err := safepath.OpenDir(".", dir, false)
	if err == nil {
		return handle.Close()
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := makeGeneratedDirs(filepath.Dir(dir), created); err != nil {
		return err
	}
	handle, err = safepath.OpenDir(".", dir, true)
	if err != nil {
		return err
	}
	*created = append(*created, dir)
	return handle.Close()
}
