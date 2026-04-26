package cmd

import (
	"fmt"

	"platformctl/internal/bootstrap"
	"platformctl/internal/config"
	"platformctl/internal/executor"
	"platformctl/internal/generator"

	"github.com/spf13/cobra"
)

func newUpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Create the platform from platform.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(defaultConfigFile)
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			runner := executor.NewRunner(cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err := runner.RequireTools("terraform", "aws", "kubectl", "helm"); err != nil {
				return err
			}

			gen := generator.New("generated")
			if err := gen.Generate(cfg); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Generated platform files in generated/")

			tf := executor.NewTerraform(runner, "generated/terraform")
			if err := tf.Init(); err != nil {
				return err
			}
			if err := tf.Plan(); err != nil {
				return err
			}
			if err := tf.Apply(); err != nil {
				return err
			}

			aws := executor.NewAWS(runner)
			if err := aws.UpdateKubeconfig(cfg.Provider.Region, cfg.Cluster.Name, cfg.Provider.Profile); err != nil {
				return err
			}

			bs := bootstrap.New(runner, "generated", cfg)
			if err := bs.Run(); err != nil {
				return err
			}

			printAccessInstructions(cmd.OutOrStdout(), cfg)
			return nil
		},
	}
}
