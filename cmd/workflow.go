package cmd

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"platformctl/internal/apperror"
	"platformctl/internal/state"
	"platformctl/internal/templateengine"
)

func acquireWorkflowLock(phase string) (*state.Lock, error) {
	lock, err := state.AcquireLock(phase)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_WORKFLOW_LOCKED", "another platformctl workflow is already running")
	}
	setupLockSignalRelease(lock)
	return lock, nil
}

func setupLockSignalRelease(lock *state.Lock) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signals
		_ = lock.Release()
		os.Exit(130)
	}()
}

func warnIfPlanChanged(resolved *templateengine.Resolved, st *state.State) []string {
	if st == nil || st.TemplateSource == "" {
		return nil
	}
	var warnings []string
	if st.TemplateChecksum != "" && resolved.Info.Checksum != "" && st.TemplateChecksum != resolved.Info.Checksum {
		warnings = append(warnings, "template checksum differs from the last recorded plan; run platformctl plan before applying")
	}
	if st.GeneratedHash != "" {
		if currentHash, err := resolved.GeneratedHash(); err == nil && currentHash != "" && currentHash != st.GeneratedHash {
			warnings = append(warnings, "generated files differ from the last recorded plan; run platformctl plan before applying")
		}
	}
	return warnings
}

func printPlanWarnings(w io.Writer, warnings []string) {
	for _, warning := range warnings {
		if options.JSON || options.Quiet {
			continue
		}
		fmt.Fprintf(w, "[warn]    %s\n", warning)
	}
}

func workflowStdout(stdout, stderr io.Writer) io.Writer {
	if options.JSON {
		return stderr
	}
	return stdout
}
