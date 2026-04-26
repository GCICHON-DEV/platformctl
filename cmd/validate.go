package cmd

import (
	"fmt"

	"platformctl/internal/config"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate platform configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(defaultConfigFile)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuration is valid: %s\n", defaultConfigFile)
			return nil
		},
	}
}
