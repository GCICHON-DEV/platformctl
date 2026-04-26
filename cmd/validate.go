package cmd

import (
	"fmt"

	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate platform configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return err
			}
			if err := resolved.Validate(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuration is valid: %s using template %s\n", defaultConfigFile, resolved.Manifest.Name)
			return nil
		},
	}
}
