package cmd

import (
	"fmt"

	"platformctl/internal/executor"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Destroy the platform from platform.yaml",
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
			fmt.Fprintf(cmd.OutOrStdout(), "Generated platform files from template %s before destroy.\n", resolved.Manifest.Name)

			if err := resolved.RunSteps(runner, resolved.Manifest.Workflow.Down); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Platform destroy workflow finished.")
			return nil
		},
	}
}
