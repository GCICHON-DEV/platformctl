package cmd

import (
	"fmt"
	"os"

	"platformctl/internal/deps"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var install bool
	var includeDev bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check local tools required by the selected template",
		RunE: func(cmd *cobra.Command, args []string) error {
			tools := []deps.Tool{}
			resolved, err := loadTemplateIfPresent(cmd)
			if err != nil {
				return err
			}
			if resolved != nil {
				for _, tool := range resolved.Manifest.Requirements.Tools {
					tools = append(tools, deps.Tool{Name: tool.Name, RequiredFor: resolved.Manifest.Name, BrewPackage: tool.Name, VersionArgs: []string{"--version"}})
				}
			}
			if includeDev {
				tools = append(tools, deps.DevTools()...)
			}
			if len(tools) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tools to check.")
				return nil
			}

			statuses := deps.Check(tools)
			printDependencyStatus(cmd, statuses)

			missing := deps.Missing(statuses)
			if len(missing) > 0 {
				if !install {
					fmt.Fprintln(cmd.OutOrStdout())
					fmt.Fprintln(cmd.OutOrStdout(), deps.ManualInstallInstructions(missing))
					fmt.Fprintln(cmd.OutOrStdout(), "\nRun platformctl check --install to install missing runtime dependencies when supported.")
					return fmt.Errorf("%d required dependencies are missing", len(missing))
				}
				if err := deps.InstallMissing(missing, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "\nRechecking dependencies...")
				statuses = deps.Check(tools)
				printDependencyStatus(cmd, statuses)
				if missing := deps.Missing(statuses); len(missing) > 0 {
					return fmt.Errorf("%d dependencies are still missing after installation", len(missing))
				}
			}

			fmt.Fprintln(cmd.OutOrStdout(), "\nAll required checks passed.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "install missing dependencies when supported")
	cmd.Flags().BoolVar(&includeDev, "dev", false, "also check Go, which is required for building from source")
	return cmd
}

func loadTemplateIfPresent(cmd *cobra.Command) (*templateengine.Resolved, error) {
	if _, err := os.Stat(defaultConfigFile); os.IsNotExist(err) {
		fmt.Fprintf(cmd.OutOrStdout(), "[skip]    %s not found; template checks skipped\n", defaultConfigFile)
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("check %s: %w", defaultConfigFile, err)
	}

	resolved, err := templateengine.Load(defaultConfigFile)
	if err != nil {
		return nil, err
	}
	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[ok]      template %s\n", resolved.Manifest.Name)
	return resolved, nil
}

func printDependencyStatus(cmd *cobra.Command, statuses []deps.Status) {
	for _, status := range statuses {
		if status.Installed {
			if status.Version != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "[ok]      %-10s %s (%s)\n", status.Tool.Name, status.Path, status.Version)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "[ok]      %-10s %s\n", status.Tool.Name, status.Path)
			}
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "[missing] %-10s required for %s\n", status.Tool.Name, status.Tool.RequiredFor)
	}
}
