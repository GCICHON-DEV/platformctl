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
		Short:         "Render files and show the platform execution plan",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl plan
  platformctl plan --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			if _, _, err := ensureToolchain(cmd, resolved, false); err != nil {
				return err
			}
			if _, err := resolved.RenderedFilePaths(); err == nil {
				printf(cmd, "Rendering generated files under generated/. Existing generated output may be replaced.\n")
			}
			st, err := state.Load()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_LOAD", "could not load local state")
			}
			written, err := resolved.GenerateWithPrevious(st.ManagedFiles)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_GENERATE_FAILED", "could not render generated files")
			}
			plan, err := resolved.BuildPlan()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_PLAN_BUILD_FAILED", "could not build execution plan")
			}
			plan.GeneratedFiles = written
			planHash, err := executionPlanHash(plan)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_PLAN_HASH", "could not hash execution plan")
			}
			hash, err := resolved.GeneratedHash()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_GENERATED_HASH", "could not hash generated files")
			}
			st.TemplateSource = resolved.Info.Resolved
			st.TemplateVersion = resolved.Info.Version
			st.TemplateChecksum = resolved.Info.Checksum
			st.GeneratedHash = hash
			st.ManagedFiles = written
			st.LastPlanHash = planHash
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
