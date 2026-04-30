package cmd

import (
	"platformctl/internal/apperror"
	"platformctl/internal/executor"
	"platformctl/internal/state"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newDestroyCmd() *cobra.Command {
	var yes bool
	var resume bool

	cmd := &cobra.Command{
		Use:           "destroy",
		Aliases:       []string{"down"},
		Short:         "Destroy the platform from platform.yaml",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl destroy
  platformctl destroy --yes
  platformctl destroy --resume`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			lock, err := acquireWorkflowLock("destroy")
			if err != nil {
				return err
			}
			defer lock.Release()
			manager, _, err := ensureToolchain(cmd, resolved, true)
			if err != nil {
				return err
			}
			runner := executor.NewRunner(workflowStdout(cmd.OutOrStdout(), cmd.ErrOrStderr()), cmd.ErrOrStderr()).WithPathEnv(manager.PathEnv())
			if err := runner.RequireTools(resolved.RequiredTools()...); err != nil {
				return apperror.Wrap(err, apperror.CategoryDependency, "PLATFORMCTL_MISSING_TOOLS", "required tools are missing")
			}
			st, err := state.Load()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_LOAD", "could not load local state")
			}
			planWarnings := warnIfPlanChanged(resolved, st)
			printPlanWarnings(cmd.OutOrStdout(), planWarnings)
			written, err := resolved.Generate()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_GENERATE_FAILED", "could not render generated files")
			}
			plan, err := resolved.BuildPlan()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_PLAN_BUILD_FAILED", "could not build execution plan")
			}
			plan.GeneratedFiles = written
			if !options.JSON {
				printSteps(cmd.OutOrStdout(), "Destroy steps", plan.DestroySteps)
			}
			if options.JSON && !yes {
				return apperror.WithRemediation(
					apperror.New(apperror.CategoryExecution, "PLATFORMCTL_CONFIRMATION_REQUIRED", "destroy requires --yes when --json is used"),
					"Run platformctl destroy --yes --json.",
				)
			}
			if !yes {
				ok, err := confirm(cmd, "Destroy this platform?")
				if err != nil {
					return err
				}
				if !ok {
					println(cmd, "Destroy cancelled.")
					return nil
				}
			}

			if !resume || st.LastPhase != "destroy" {
				state.ResetPhase(st, "destroy")
			}
			hash, err := resolved.GeneratedHash()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_GENERATED_HASH", "could not hash generated files")
			}
			st.TemplateSource = resolved.Info.Resolved
			st.TemplateVersion = resolved.Info.Version
			st.TemplateChecksum = resolved.Info.Checksum
			st.GeneratedHash = hash
			if err := state.Save(st); err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_SAVE", "could not save local state")
			}

			if err := resolved.RunSteps(runner, "destroy", st.CompletedSteps, func(stepID string) error {
				st.CompletedSteps[stepID] = true
				st.LastPhase = "destroy"
				return state.Save(st)
			}); err != nil {
				return err
			}
			println(cmd, "Destroy workflow finished.")
			if options.JSON {
				writeJSON(cmd.OutOrStdout(), map[string]interface{}{"ok": true, "phase": "destroy", "generated_files": written, "warnings": planWarnings})
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "approve and run without interactive confirmation")
	cmd.Flags().BoolVar(&resume, "resume", false, "skip destroy steps already completed in the last destroy run")
	return cmd
}
