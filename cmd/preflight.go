package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"platformctl/internal/apperror"
	"platformctl/internal/deps"
	"platformctl/internal/templateengine"
	"platformctl/internal/toolchain"

	"github.com/spf13/cobra"
)

type preflightResult struct {
	OK          bool          `json:"ok"`
	Template    string        `json:"template"`
	Source      string        `json:"source"`
	Checksum    string        `json:"checksum,omitempty"`
	Tools       []deps.Status `json:"tools,omitempty"`
	Credentials []checkStatus `json:"credentials,omitempty"`
	Warnings    []string      `json:"warnings,omitempty"`
	Checks      []checkStatus `json:"checks,omitempty"`
}

type checkStatus struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func newPreflightCmd() *cobra.Command {
	var strict bool

	cmd := &cobra.Command{
		Use:           "preflight",
		Aliases:       []string{"doctor"},
		Short:         "Check template dependencies, credentials, and local readiness",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl preflight
  platformctl preflight --strict
  platformctl preflight --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			result, err := runPreflight(cmd, resolved, strict)
			if err != nil {
				return err
			}
			if !result.OK && strict {
				return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_PREFLIGHT_FAILED", "preflight checks failed")
			}
			if options.JSON {
				writeJSON(cmd.OutOrStdout(), result)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "fail if any required dependency, version, or credential check fails")
	return cmd
}

func runPreflight(cmd *cobra.Command, resolved *templateengine.Resolved, strict bool) (*preflightResult, error) {
	result := &preflightResult{OK: true, Template: resolved.Manifest.Name, Source: resolved.Info.Resolved, Checksum: resolved.Info.Checksum}
	printf(cmd, "Template: %s\n", resolved.Manifest.Name)
	printf(cmd, "Source:   %s\n", resolved.Info.Resolved)
	if resolved.Info.Checksum != "" {
		printf(cmd, "Checksum: %s\n", resolved.Info.Checksum)
	}

	manager, statuses, err := ensureToolchain(cmd, resolved, strict)
	if err != nil {
		return result, apperror.Wrap(err, apperror.CategoryDependency, "PLATFORMCTL_TOOLCHAIN_FAILED", "toolchain preflight failed")
	}
	result.Tools = statuses
	if missing := deps.Missing(statuses); len(missing) > 0 {
		result.OK = false
		if strict {
			return result, apperror.New(apperror.CategoryDependency, "PLATFORMCTL_MISSING_TOOLS", fmt.Sprintf("%d required dependencies are missing", len(missing)))
		}
	}
	for _, status := range statuses {
		if problem := deps.VersionProblem(status); problem != "" {
			result.OK = false
			result.Warnings = append(result.Warnings, problem)
			if strict {
				return result, apperror.New(apperror.CategoryDependency, "PLATFORMCTL_VERSION_MISMATCH", problem)
			}
			printf(cmd, "[warn]    %s\n", problem)
		}
	}

	for _, credential := range resolved.Manifest.Requirements.Credentials {
		if credential.Command == "" {
			result.Credentials = append(result.Credentials, checkStatus{Name: credential.Name, OK: true, Message: credential.Description})
			printf(cmd, "[info]    credential %s: %s\n", credential.Name, credential.Description)
			continue
		}
		command, err := resolved.Render(credential.Command)
		if err != nil {
			return result, apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_CREDENTIAL_RENDER", "credential check could not be rendered")
		}
		check := exec.Command("sh", "-c", command)
		check.Env = appendPath("PATH", manager.PathEnv())
		if err := check.Run(); err != nil {
			result.OK = false
			message := fmt.Sprintf("credential check %s failed: %v", credential.Name, err)
			result.Credentials = append(result.Credentials, checkStatus{Name: credential.Name, OK: false, Message: message})
			if strict {
				return result, apperror.New(apperror.CategoryDependency, "PLATFORMCTL_CREDENTIAL_FAILED", message)
			}
			printf(cmd, "[warn]    %s\n", message)
			continue
		}
		result.Credentials = append(result.Credentials, checkStatus{Name: credential.Name, OK: true})
		printf(cmd, "[ok]      credential %s\n", credential.Name)
	}

	if _, err := resolved.RenderedFilePaths(); err != nil {
		return result, apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_RENDER_PATHS", "template generated paths are invalid")
	}
	for _, check := range localReadinessChecks(manager.PathEnv(), resolved) {
		if !check.OK {
			result.OK = false
			result.Warnings = append(result.Warnings, check.Message)
		}
		result.Checks = append(result.Checks, check)
		if check.OK {
			printf(cmd, "[ok]      %s\n", check.Name)
		} else {
			printf(cmd, "[warn]    %s: %s\n", check.Name, check.Message)
		}
	}
	return result, nil
}

func printDependencyStatus(cmd *cobra.Command, statuses []deps.Status) {
	if options.JSON || options.Quiet {
		return
	}
	for _, status := range statuses {
		if status.Installed {
			if status.Version != "" {
				if status.Tool.MinVersion != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "[ok]      %-10s %s (%s, required >= %s)\n", status.Tool.Name, status.Path, status.Version, status.Tool.MinVersion)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "[ok]      %-10s %s (%s)\n", status.Tool.Name, status.Path, status.Version)
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "[ok]      %-10s %s\n", status.Tool.Name, status.Path)
			}
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "[missing] %-10s required for %s\n", status.Tool.Name, status.Tool.RequiredFor)
	}
}

func localReadinessChecks(pathEnv string, resolved *templateengine.Resolved) []checkStatus {
	var checks []checkStatus
	checks = append(checks, writablePathCheck("managed toolchain directory"))
	if hasTool(resolved, "docker") {
		checks = append(checks, commandCheck("docker daemon", pathEnv, "docker", "info"))
	}
	if hasTool(resolved, "kubectl") {
		checks = append(checks, commandCheck("kubectl current context", pathEnv, "kubectl", "config", "current-context"))
	}
	return checks
}

func writablePathCheck(name string) checkStatus {
	dir, err := toolchain.BinDir()
	if err != nil {
		return checkStatus{Name: name, OK: false, Message: err.Error()}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return checkStatus{Name: name, OK: false, Message: err.Error()}
	}
	probe, err := os.CreateTemp(dir, ".platformctl-write-test-*")
	if err != nil {
		return checkStatus{Name: name, OK: false, Message: err.Error()}
	}
	probeName := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probeName)
	return checkStatus{Name: name, OK: true, Message: filepath.Clean(dir) + " is writable"}
}

func commandCheck(name, pathEnv, command string, args ...string) checkStatus {
	commandPath, err := lookPathInEnv(pathEnv, command)
	if err != nil {
		return checkStatus{Name: name, OK: false, Message: err.Error()}
	}
	check := exec.Command(commandPath, args...)
	check.Env = appendPath("PATH", pathEnv)
	if err := check.Run(); err != nil {
		return checkStatus{Name: name, OK: false, Message: err.Error()}
	}
	return checkStatus{Name: name, OK: true}
}

func lookPathInEnv(pathEnv, command string) (string, error) {
	if pathEnv == "" {
		return exec.LookPath(command)
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, command)
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() && stat.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func hasTool(resolved *templateengine.Resolved, name string) bool {
	for _, tool := range resolved.RequirementTools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}
