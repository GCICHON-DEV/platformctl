package cmd

import (
	"bufio"
	"fmt"
	"strings"

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
		Short:         "Create or update the platform from an approved plan",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl apply
  platformctl apply --yes
  platformctl apply --resume`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			lock, err := acquireWorkflowLock("apply")
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
				printExecutionPlan(cmd.OutOrStdout(), plan)
			}
			if options.JSON && !yes {
				return apperror.WithRemediation(
					apperror.New(apperror.CategoryExecution, "PLATFORMCTL_CONFIRMATION_REQUIRED", "apply requires --yes when --json is used"),
					"Run platformctl apply --yes --json.",
				)
			}
			if !yes {
				ok, err := confirm(cmd, "Apply this platform plan?")
				if err != nil {
					return err
				}
				if !ok {
					println(cmd, "Apply cancelled.")
					return nil
				}
			}

			if !resume || st.LastPhase != "apply" {
				state.ResetPhase(st, "apply")
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

			if err := resolved.RunSteps(runner, "apply", st.CompletedSteps, func(stepID string) error {
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
				writeJSON(cmd.OutOrStdout(), map[string]interface{}{
					"ok":               true,
					"phase":            "apply",
					"generated_files":  written,
					"success_messages": resolved.SuccessMessages(),
					"notes":            resolved.NoteMessages(),
					"next_steps":       resolved.NextStepMessages(),
					"warnings":         planWarnings,
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
