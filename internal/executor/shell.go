package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type StepOptions struct {
	ID             string
	Name           string
	Dir            string
	Command        string
	Args           []string
	TimeoutSeconds int
	Suggestion     string
	ResumeCommand  string
}

type StepError struct {
	ID            string
	Name          string
	Dir           string
	Command       string
	Duration      time.Duration
	Suggestion    string
	ResumeCommand string
	Err           error
}

func (e *StepError) Error() string {
	return fmt.Sprintf("step %s failed after %s: %v", e.ID, e.Duration.Round(time.Second), e.Err)
}

func (e *StepError) Unwrap() error {
	return e.Err
}

func (r *Runner) RunStep(step string, dir string, name string, args ...string) error {
	return r.RunStepWithOptions(StepOptions{Name: step, Dir: dir, Command: name, Args: args})
}

func (r *Runner) RunStepWithOptions(options StepOptions) error {
	if err := validateStepDir(options.Dir); err != nil {
		return err
	}

	timeout := time.Duration(options.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	started := time.Now()
	if options.ID != "" {
		fmt.Fprintf(r.Stdout, "\n==> %s (%s)\n", options.Name, options.ID)
	} else {
		fmt.Fprintf(r.Stdout, "\n==> %s\n", options.Name)
	}
	fmt.Fprintf(r.Stdout, "$ %s\n", MaskCommand(options.Command, options.Args))

	commandPath, err := r.lookPath(options.Command)
	if err != nil {
		return stepError(options, started, fmt.Errorf("command %s not found", options.Command))
	}
	cmd := exec.CommandContext(ctx, commandPath, options.Args...)
	if r.PathEnv != "" {
		cmd.Env = appendPathEnv(r.PathEnv)
	}
	if options.Dir != "" && options.Dir != "." {
		cmd.Dir = options.Dir
	}
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return stepError(options, started, fmt.Errorf("timed out after %s", timeout))
		}
		return stepError(options, started, err)
	}
	return nil
}

func stepError(options StepOptions, started time.Time, err error) error {
	dir := options.Dir
	if dir == "" {
		dir = "."
	}
	return &StepError{
		ID:            options.ID,
		Name:          options.Name,
		Dir:           dir,
		Command:       MaskCommand(options.Command, options.Args),
		Duration:      time.Since(started),
		Suggestion:    options.Suggestion,
		ResumeCommand: options.ResumeCommand,
		Err:           err,
	}
}

func appendPathEnv(pathEnv string) []string {
	env := os.Environ()
	for i, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			env[i] = "PATH=" + pathEnv
			return env
		}
	}
	return append(env, "PATH="+pathEnv)
}

func validateStepDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	clean := filepath.Clean(dir)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("step dir %q must be relative to the workspace", dir)
	}
	if clean != "generated" && !strings.HasPrefix(clean, "generated"+string(os.PathSeparator)) {
		return fmt.Errorf("step dir %q is not allowed; workflow steps may run only in generated/", dir)
	}
	return nil
}

func MaskCommand(command string, args []string) string {
	parts := append([]string{command}, args...)
	return mask(strings.Join(parts, " "))
}

func mask(text string) string {
	if text == "" {
		return text
	}
	fields := []string{"password", "secret", "token", "key"}
	out := text
	for _, field := range fields {
		out = maskAssignments(out, field)
	}
	return out
}

func maskAssignments(text, field string) string {
	parts := strings.Fields(text)
	for i, part := range parts {
		lower := strings.ToLower(part)
		if strings.Contains(lower, field+"=") || strings.Contains(lower, field+":") {
			if idx := strings.IndexAny(part, "=:"); idx >= 0 {
				parts[i] = part[:idx+1] + "******"
			}
		}
	}
	return strings.Join(parts, " ")
}
