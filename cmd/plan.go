package cmd

import (
	"platformctl/internal/apperror"
	"platformctl/internal/state"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newPlanCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "plan",
		Short:         "Render a CLI workflow preview (does not run terraform plan)",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl plan
  platformctl plan --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			lock, err := acquireWorkflowLock("plan")
			if err != nil {
				return err
			}
			defer lock.Release()
			resolved, err := templateengine.Load(defaultConfigFile)
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
			written, err := plan.GenerateWithPrevious(st.ManagedFiles)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_GENERATE_FAILED", "could not render preview files")
			}
			planHash := plan.Digest
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
			st.ApprovedPlanHash = ""
			st.ApprovedPhase = ""
			st.LastPhase = "plan"
			st.CompletedSteps = map[string]bool{}
			if err := state.Save(st); err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_SAVE", "could not save local state")
			}
			printExecutionPlan(cmd.OutOrStdout(), plan)
			if !options.JSON {
				printf(cmd, "Plan rendered. State written to %s.\n", state.Path())
			}
			return nil
		},
	}
}
