//go:build !darwin && !linux

package checkexec

import "os/exec"

func configure(cmd *exec.Cmd) func() { return func() {} }
