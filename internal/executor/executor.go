package executor

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type Runner struct {
	Stdout io.Writer
	Stderr io.Writer
}

func NewRunner(stdout, stderr io.Writer) *Runner {
	return &Runner{Stdout: stdout, Stderr: stderr}
}

func (r *Runner) RequireTools(tools ...string) error {
	var missing []string
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required tools: %s\nRun platformctl check --install to install missing dependencies when supported", strings.Join(missing, ", "))
	}
	return nil
}
