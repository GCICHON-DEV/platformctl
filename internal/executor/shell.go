package executor

import (
	"fmt"
	"os/exec"
	"strings"
)

func (r *Runner) RunStep(step string, dir string, name string, args ...string) error {
	fmt.Fprintf(r.Stdout, "\n==> %s\n", step)
	fmt.Fprintf(r.Stdout, "$ %s %s\n", name, strings.Join(args, " "))

	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", step, err)
	}
	return nil
}
