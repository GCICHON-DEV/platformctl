package executor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Runner struct {
	Stdout  io.Writer
	Stderr  io.Writer
	PathEnv string
}

func NewRunner(stdout, stderr io.Writer) *Runner {
	return &Runner{Stdout: stdout, Stderr: stderr}
}

func (r *Runner) WithPathEnv(pathEnv string) *Runner {
	r.PathEnv = pathEnv
	return r
}

func (r *Runner) RequireTools(tools ...string) error {
	var missing []string
	for _, tool := range tools {
		if _, err := r.lookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tools: %s\nRun platformctl init to see template preflight details, then install the missing tools", strings.Join(missing, ", "))
	}
	return nil
}

func (r *Runner) lookPath(tool string) (string, error) {
	if r.PathEnv == "" {
		return exec.LookPath(tool)
	}
	for _, dir := range filepath.SplitList(r.PathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, tool)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() && stat.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}
