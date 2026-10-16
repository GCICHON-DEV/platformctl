package cmd

import (
	"io"

	"platformctl/internal/apperror"
	"platformctl/internal/state"
	"platformctl/internal/templateengine"
)

func acquireWorkflowLock(phase string) (*state.Lock, error) {
	lock, err := state.AcquireLock(phase)
	if err != nil {
		return nil, apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_WORKFLOW_LOCKED", "another platformctl workflow is already running")
	}
	return lock, nil
}

func workflowStdout(stdout, stderr io.Writer) io.Writer {
	if options.JSON {
		return stderr
	}
	return stdout
}

// validateRecordedPlan never updates files or state. A preview is not approval;
// only a saved approval of this phase can authorize checkpoint reuse.
func validateRecordedPlan(resolved *templateengine.Resolved, plan *templateengine.ExecutionPlan, st *state.State, phase string, resume bool) error {
	code := "PLATFORMCTL_PLAN_CHANGED"
	if resume {
		code = "PLATFORMCTL_RESUME_INVALID"
	}
	invalid := func(reason string) error {
		return apperror.WithRemediation(apperror.New(apperror.CategoryState, code, reason), "Run platformctl plan to review the new preview, then run platformctl "+phase+" without --resume.")
	}
	if resume && (st.PlanFormat != templateengine.PlanFormat || st.ApprovedPlanHash == "" || st.ApprovedPhase != phase || st.LastPhase != phase || st.ApprovedPlanHash != plan.Digest) {
		return invalid("resume requires an unchanged preview approved for this workflow phase")
	}
	if st.LastPlanHash != "" && (st.PlanFormat != templateengine.PlanFormat || st.LastPlanHash != plan.Digest) {
		return invalid("execution preview changed since it was recorded")
	}
	if st.LastPlanHash != "" {
		current, err := resolved.GeneratedHash()
		if err != nil || current != st.GeneratedHash || current != plan.GeneratedDigest {
			return invalid("generated files changed or are missing since the recorded preview")
		}
	}
	return nil
}

// Check immediately before mutation and each step, including target config edits
// made by external tools while the CLI held its cooperative workspace lock.
func verifyPlanSnapshot(plan *templateengine.ExecutionPlan) (*templateengine.Resolved, error) {
	current, err := templateengine.Load(defaultConfigFile)
	if err != nil {
		return nil, err
	}
	if err := current.Validate(); err != nil {
		return nil, err
	}
	check, err := current.BuildPlan()
	if err != nil {
		return nil, err
	}
	if check.Digest != plan.Digest {
		return nil, apperror.WithRemediation(apperror.New(apperror.CategoryState, "PLATFORMCTL_PLAN_CHANGED", "preview changed after approval; review it again"), "Run platformctl plan, then approve the new preview without --resume.")
	}
	return current, nil
}
