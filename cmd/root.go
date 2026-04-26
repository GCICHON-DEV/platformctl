package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const defaultConfigFile = "platform.yaml"

var rootCmd = &cobra.Command{
	Use:   "platformctl",
	Short: "Create a real AWS developer platform with simple commands",
	Long: `platformctl is a guided bootstrapper for a real AWS developer platform.

It uses Terraform, AWS CLI, kubectl, and Helm under the hood so developers can
create, inspect, and destroy a working platform without manually stitching each
DevOps step together.`,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(newCheckCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newUpCmd())
	rootCmd.AddCommand(newDownCmd())
}
