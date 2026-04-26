package cmd

import (
	"fmt"

	"platformctl/internal/config"
	"platformctl/internal/executor"
	"platformctl/internal/generator"

	"github.com/spf13/cobra"
)

func newDownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Destroy the platform from platform.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(defaultConfigFile)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			runner := executor.NewRunner(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err := runner.RequireTools("terraform"); err != nil {
				return err
			}

			gen := generator.New("generated")
			if err := gen.Generate(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Generated Terraform files before destroy.")

			tf := executor.NewTerraform(runner, "generated/terraform")
			if err := tf.Init(); err != nil {
				return err
			}
			if err := tf.Destroy(); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Terraform destroy finished.")
			return nil
		},
	}
}
