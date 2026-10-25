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
	Stdout       io.Writer
	Stderr       io.Writer
	PathEnv      string
	WorkflowLock *os.File
	Environment  map[string]string
}

func NewRunner(stdout, stderr io.Writer) *Runner {
	return &Runner{Stdout: stdout, Stderr: stderr}
}

func (r *Runner) WithPathEnv(pathEnv string) *Runner {
	r.PathEnv = pathEnv
	return r
}

// WithWorkflowLock keeps surviving workflow steps exclusive after a CLI crash.
func (r *Runner) WithWorkflowLock(file *os.File) *Runner {
	r.WorkflowLock = file
	return r
}

// WithEnvironment supplies process-local overrides; it never changes the CLI environment.
func (r *Runner) WithEnvironment(values map[string]string) *Runner { r.Environment = values; return r }

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
