package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"platformctl/internal/config"
	"platformctl/internal/deps"

	"github.com/spf13/cobra"
)

func newCheckCmd() *cobra.Command {
	var install bool
	var includeDev bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check local tools and AWS access",
		RunE: func(cmd *cobra.Command, args []string) error {
			tools := deps.RuntimeTools()
			if includeDev {
				tools = append(tools, deps.DevTools()...)
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

			if err := checkConfigAndAWS(cmd); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "\nAll required checks passed.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&install, "install", false, "install missing dependencies when supported")
	cmd.Flags().BoolVar(&includeDev, "dev", false, "also check Go, which is required for building from source")
	return cmd
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

func checkConfigAndAWS(cmd *cobra.Command) error {
	if _, err := os.Stat(defaultConfigFile); os.IsNotExist(err) {
		fmt.Fprintf(cmd.OutOrStdout(), "\n[skip]    %s not found; config and AWS identity check skipped\n", defaultConfigFile)
		return nil
	} else if err != nil {
		return fmt.Errorf("check %s: %w", defaultConfigFile, err)
	}

	cfg, err := config.Load(defaultConfigFile)
	if err != nil {
		return err
	}
	if cfg.Provider.Name != "aws" {
		return fmt.Errorf("provider.name must be aws before AWS identity can be checked")
	}
	if cfg.Provider.Region == "" {
		return fmt.Errorf("provider.region is required before AWS identity can be checked")
	}

	args := []string{"sts", "get-caller-identity", "--region", cfg.Provider.Region}
	if cfg.Provider.Profile != "" {
		args = append(args, "--profile", cfg.Provider.Profile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	aws := exec.CommandContext(ctx, "aws", args...)
	var out bytes.Buffer
	aws.Stdout = &out
	aws.Stderr = &out
	if err := aws.Run(); err != nil {
		output := strings.TrimSpace(out.String())
		if output != "" {
			return fmt.Errorf("AWS identity check failed: %w\n%s", err, output)
		}
		return fmt.Errorf("AWS identity check failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "[ok]      aws identity for profile %q in %s\n", cfg.Provider.Profile, cfg.Provider.Region)
	return nil
}
