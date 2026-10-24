// Package checkexec runs bounded, non-interactive diagnostic commands. It does
// not classify a command as read-only: credential commands are trusted template code.
package checkexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

const OutputLimit = 32 * 1024

var ErrOutputLimit = errors.New("check output exceeds 32 KiB")

// Run captures both streams under one cap. No stdin or caller output writer is
// inherited. Timeout includes descendants holding stdout/stderr open.
func Run(ctx context.Context, timeout time.Duration, path string, args, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cleanup := configure(cmd)
	defer cleanup()
	cmd.WaitDelay = 100 * time.Millisecond
	output := &limitedOutput{cancel: cancel}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	if output.exceeded {
		return nil, ErrOutputLimit
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("check deadline/cancellation: %w", ctx.Err())
	}
	if err != nil {
		return nil, fmt.Errorf("check process failed: %w", err)
	}
	return output.buffer.Bytes(), nil
}

type limitedOutput struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	cancel   context.CancelFunc
	exceeded bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.exceeded || len(p) > OutputLimit-w.buffer.Len() {
		w.exceeded = true
		w.cancel()
		return 0, ErrOutputLimit
	}
	return w.buffer.Write(p)
}
