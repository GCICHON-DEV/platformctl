package cmd

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"platformctl/internal/apperror"
	"platformctl/internal/executor"
	"platformctl/internal/state"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var yes bool
	var resume bool

	cmd := &cobra.Command{
		Use:           "apply",
		Aliases:       []string{"up"},
		Short:         "Preview and approve a platform workflow before execution",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl apply
  platformctl apply --yes
  platformctl apply --resume`,
		RunE: func(cmd *cobra.Command, args []string) error {
			lock, err := acquireWorkflowLock("apply")
			if err != nil {
				return err
			}
			defer lock.Release()
			resolved, err := templateengine.LoadWithMode(defaultConfigFile, options.Offline)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			st, err := state.Load()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_LOAD", "could not load local state")
			}
			plan, err := resolved.BuildPlan()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_PLAN_BUILD_FAILED", "could not build execution plan")
			}
			planHash := plan.Digest
			if err := validateRecordedPlan(resolved, plan, st, "apply", resume); err != nil {
				return err
			}
			if !options.JSON {
				printExecutionPlan(cmd.OutOrStdout(), plan)
			}
			if options.JSON && !yes {
				return apperror.WithRemediation(
					apperror.New(apperror.CategoryExecution, "PLATFORMCTL_CONFIRMATION_REQUIRED", "apply requires --yes when --json is used"),
					"Run platformctl apply --yes --json.",
				)
			}
			if !yes {
				ok, err := confirm(cmd, "Approve this CLI workflow preview and apply?")
				if err != nil {
					return err
				}
				if !ok {
					println(cmd, "Apply cancelled.")
					return nil
				}
			}
			// Re-read after interactive confirmation: external editors do not take
			// the workflow lock. Execute the captured bytes/steps only.
			current, err := verifyPlanSnapshot(plan)
			if err != nil {
				return err
			}
			if err := validateRecordedPlan(current, plan, st, "apply", resume); err != nil {
				return err
			}
			if err := requireDeclaredTarget(resolved); err != nil {
				return err
			}
			bootstrap, err := prepareBootstrap(resolved, plan, st, resume)
			if err != nil {
				return err
			}
			for _, step := range plan.ApplySteps {
				if err := validateStepTarget(resolved, step); err != nil {
					return err
				}
			}
			manager, _, err := ensureToolchain(cmd, resolved, true)
			if err != nil {
				return err
			}
			phase := "apply"
			if resolved.Platform.Target != nil && resolved.Platform.Target.Bootstrap {
				phase = "bootstrap"
			}
			if bootstrap != nil && bootstrap.resumed {
				phase = "apply"
			}
			preflight, err := runPreflightPhase(cmd, resolved, true, phase, 5*time.Second)
			if err != nil {
				return err
			}
			if !preflight.OK {
				return preflightFailure(preflight)
			}
			runner := executor.NewRunner(workflowStdout(cmd.OutOrStdout(), cmd.ErrOrStderr()), cmd.ErrOrStderr()).WithPathEnv(manager.PathEnv()).WithWorkflowLock(lock.File()).WithEnvironment(targetEnvironment(resolved.Platform.Target))
			if err := runner.RequireTools(resolved.RequiredTools()...); err != nil {
				return apperror.Wrap(err, apperror.CategoryDependency, "PLATFORMCTL_MISSING_TOOLS", "required tools are missing")
			}
			current, err = verifyPlanSnapshot(plan)
			if err != nil {
				return err
			}
			if err := validateRecordedPlan(current, plan, st, "apply", resume); err != nil {
				return err
			}
			written, err := plan.GenerateWithPrevious(st.ManagedFiles)
			if err != nil {
				return apperror.WithContext(apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_GENERATE_FAILED", "could not write approved preview"), err.Error())
			}

			if !resume || st.LastPhase != "apply" {
				state.ResetPhase(st, "apply")
			}
			hash, err := resolved.GeneratedHash()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_GENERATED_HASH", "could not hash generated files")
			}
			if hash != plan.GeneratedDigest {
				return apperror.New(apperror.CategoryState, "PLATFORMCTL_PLAN_CHANGED", "generated files no longer match captured preview bytes")
			}
			st.TemplateSource = resolved.Info.Resolved
			st.TemplateVersion = resolved.Info.Version
			st.TemplateChecksum = resolved.Info.Checksum
			st.GeneratedHash = hash
			st.ManagedFiles = written
			st.LastPlanHash = planHash
			st.PlanFormat = plan.Format
			st.ApprovedPlanHash = planHash
			st.ApprovedPhase = "apply"
			if err := state.Save(st); err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_SAVE", "could not save local state")
			}

			if err := templateengine.RunPlannedStepsGuarded(runner, "apply", plan.ApplySteps, st.CompletedSteps, func() error {
				current, err := verifyPlanSnapshot(plan)
				if err != nil {
					return err
				}
				return validateRecordedPlan(current, plan, st, "apply", resume)
			}, func(step templateengine.PlannedStep) error {
				if err := validateStepTarget(resolved, step); err != nil {
					return err
				}
				if bootstrap != nil {
					return bootstrap.before(cmd.Context(), manager.PathEnv(), resolved, st, step)
				}
				return requireTargetAt(cmd.Context(), manager.PathEnv(), resolved, phase != "bootstrap" || clusterStep(step), step.Dir)
			}, func(stepID string) error {
				if bootstrap != nil {
					if err := bootstrap.after(cmd.Context(), manager.PathEnv(), resolved, plan, st, stepID); err != nil {
						return err
					}
				}
				st.CompletedSteps[stepID] = true
				st.LastPhase = "apply"
				return state.Save(st)
			}); err != nil {
				return err
			}
			for _, message := range resolved.SuccessMessages() {
				println(cmd, message)
			}
			for _, message := range resolved.NoteMessages() {
				printf(cmd, "Note: %s\n", message)
			}
			for _, message := range resolved.NextStepMessages() {
				printf(cmd, "Next: %s\n", message)
			}
			println(cmd, "Apply workflow finished.")
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]interface{}{
					"ok":               true,
					"phase":            "apply",
					"generated_files":  written,
					"success_messages": resolved.SuccessMessages(),
					"notes":            resolved.NoteMessages(),
					"next_steps":       resolved.NextStepMessages(),
					"warnings":         plan.Warnings,
				})
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve and run without interactive confirmation")
	cmd.Flags().BoolVar(&resume, "resume", false, "skip apply steps already completed in the last apply run")
	return cmd
}

func confirm(cmd *cobra.Command, prompt string) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", prompt)
	reader := bufio.NewReader(cmd.InOrStdin())
	text, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	text = strings.TrimSpace(strings.ToLower(text))
	return text == "y" || text == "yes", nil
}
