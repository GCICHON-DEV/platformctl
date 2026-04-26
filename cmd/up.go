package cmd

import (
	"fmt"

	"platformctl/internal/executor"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Create the platform from platform.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return err
			}
			if err := resolved.Validate(); err != nil {
				return err
			}

			runner := executor.NewRunner(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err := runner.RequireTools(resolved.RequiredTools()...); err != nil {
				return err
			}

			if err := resolved.Generate(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Generated platform files from template %s.\n", resolved.Manifest.Name)

			if err := resolved.RunSteps(runner, resolved.Manifest.Workflow.Up); err != nil {
				return err
			}

			for _, message := range resolved.SuccessMessages() {
				fmt.Fprintln(cmd.OutOrStdout(), message)
			}
			return nil
		},
	}
}
