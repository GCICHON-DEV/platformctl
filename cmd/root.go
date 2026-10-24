package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

const defaultConfigFile = "platform.yaml"

type cliOptions struct {
	Verbose bool
	Quiet   bool
	NoColor bool
	JSON    bool
}

var options cliOptions

var rootCmd = &cobra.Command{
	Use:   "platformctl",
	Short: "Bootstrap developer platforms from declarative templates",
	Long: `platformctl renders platform templates, verifies dependencies, and runs
controlled platform workflows for local and cloud Kubernetes environments.

It is designed for platform engineering workflows where repeatability, clear
preflight checks, resumable execution, and safe generated output matter.`,
	Example: `  platformctl init --template platformctl/local-kind-standard --project demo
  platformctl preflight
  platformctl plan
  platformctl apply --yes
  platformctl apply --resume
  platformctl status --json
  platformctl destroy`,
	SilenceUsage:  true,
	SilenceErrors: true,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

func Execute() {
	os.Exit(runCLI(rootCmd, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&options.Verbose, "verbose", false, "show technical causes when an operation fails")
	rootCmd.PersistentFlags().BoolVar(&options.Quiet, "quiet", false, "print only warnings, errors, and requested machine-readable output")
	rootCmd.PersistentFlags().BoolVar(&options.NoColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVar(&options.JSON, "json", false, "write one machine-readable JSON document to stdout")

	rootCmd.AddCommand(newInitCmd())
	rootCmd.AddCommand(newPreflightCmd())
	rootCmd.AddCommand(newPlanCmd())
	rootCmd.AddCommand(newApplyCmd())
	rootCmd.AddCommand(newDestroyCmd())
	rootCmd.AddCommand(newStatusCmd())
}
