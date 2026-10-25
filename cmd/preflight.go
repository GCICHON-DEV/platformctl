package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"platformctl/internal/checkexec"
	"platformctl/internal/state"
	"strings"
	"time"

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
	Phase       string        `json:"phase"`
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
	var phase string
	var timeout time.Duration

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
			resolved, err := templateengine.LoadWithMode(defaultConfigFile, options.Offline)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			if phase != "bootstrap" && phase != "apply" && phase != "destroy" {
				return apperror.New(apperror.CategoryConfig, "PLATFORMCTL_PREFLIGHT_PHASE", "phase must be bootstrap, apply, or destroy")
			}
			if timeout < 100*time.Millisecond || timeout > 30*time.Second {
				return apperror.New(apperror.CategoryConfig, "PLATFORMCTL_PREFLIGHT_TIMEOUT", "check timeout must be between 100ms and 30s")
			}
			result, err := runPreflightPhase(cmd, resolved, strict, phase, timeout)
			if err != nil {
				return err
			}
			if !result.OK && strict {
				return preflightFailure(result)
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "fail if any required dependency, version, or credential check fails")
	cmd.Flags().StringVar(&phase, "phase", "bootstrap", "readiness phase: bootstrap, apply, or destroy")
	cmd.Flags().DurationVar(&timeout, "check-timeout", 5*time.Second, "deadline for each diagnostic subprocess (100ms..30s)")
	return cmd
}

func runPreflight(cmd *cobra.Command, resolved *templateengine.Resolved, strict bool) (*preflightResult, error) {
	return runPreflightPhase(cmd, resolved, strict, "bootstrap", 5*time.Second)
}

func runPreflightPhase(cmd *cobra.Command, resolved *templateengine.Resolved, strict bool, phase string, timeout time.Duration) (*preflightResult, error) {
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()
	result := &preflightResult{Phase: phase, OK: true, Template: resolved.Manifest.Name, Source: resolved.Info.Resolved, Checksum: resolved.Info.Checksum}
	printf(cmd, "Template: %s\n", resolved.Manifest.Name)
	printf(cmd, "Source:   %s\n", resolved.Info.Resolved)
	if resolved.Info.Checksum != "" {
		printf(cmd, "Checksum: %s\n", resolved.Info.Checksum)
	}

	missingIdentity := requiredTargetChecks(resolved)
	if len(missingIdentity) > 0 {
		result.OK = false
		result.Checks = append(result.Checks, missingIdentity...)
		if strict {
			return result, requireDeclaredTarget(resolved)
		}
	}
	if resolved.Platform.Target != nil && resolved.Platform.Target.Bootstrap {
		plan, err := resolved.BuildPlan()
		if err != nil {
			return result, err
		}
		if _, err := bootstrapDefinition(resolved, plan); err != nil {
			return result, err
		}
	}
	manager, err := toolchain.New(nil, nil)
	if err != nil {
		return result, apperror.Wrap(err, apperror.CategoryDependency, "PLATFORMCTL_TOOLCHAIN_FAILED", "toolchain preflight failed")
	}
	statuses := manager.Check(ctx, timeout, resolved.RequirementTools())
	printDependencyStatus(cmd, statuses)
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
		args := make([]string, 0, len(credential.Args))
		for _, arg := range credential.Args {
			rendered, err := resolved.Render(arg)
			if err != nil {
				return result, apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_CREDENTIAL_RENDER", "credential check arguments could not be rendered")
			}
			args = append(args, rendered)
		}
		commandPath, checkErr := lookPathInEnv(manager.PathEnv(), command)
		if checkErr == nil {
			_, checkErr = checkexec.Run(ctx, timeout, commandPath, args, appendPath("PATH", manager.PathEnv()))
		}
		if err := checkErr; err != nil {
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
	checks := localReadinessChecks(ctx, timeout, manager.PathEnv(), resolved, phase)
	checks = append(checks, targetChecks(ctx, timeout, manager.PathEnv(), resolved, phase != "bootstrap")...)
	workflowPhase := phase
	if workflowPhase == "bootstrap" {
		workflowPhase = "apply"
	}
	seenDirs := map[string]bool{"": true, ".": true}
	for _, step := range resolved.StepsFor(workflowPhase) {
		directory, err := resolved.Render(step.Dir)
		if err != nil {
			checks = append(checks, checkStatus{Name: "step working directory", Message: "could not render step working directory"})
			continue
		}
		if seenDirs[directory] {
			continue
		}
		seenDirs[directory] = true
		checks = append(checks, targetChecksAt(ctx, timeout, manager.PathEnv(), resolved, phase != "bootstrap", directory)...)
	}
	identityWarnings := []string{}
	if resolved.Platform.Target == nil {
		identityWarnings = append(identityWarnings, "target identity is not verified: legacy config has no expected target")
	} else {
		if hasTool(resolved, "aws") && resolved.Platform.Target.AWS == nil {
			identityWarnings = append(identityWarnings, "AWS account/region identity is not verified: no target.aws")
		}
		if hasTool(resolved, "kubectl") && resolved.Platform.Target.Kubernetes == nil {
			identityWarnings = append(identityWarnings, "Kubernetes identity is not verified: no target.kubernetes")
		}
	}
	for _, message := range identityWarnings {
		result.Warnings = append(result.Warnings, message)
		printf(cmd, "[warn]    %s\n", message)
	}

	for _, check := range checks {
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

func localReadinessChecks(ctx context.Context, timeout time.Duration, pathEnv string, resolved *templateengine.Resolved, phase string) []checkStatus {
	checks := []checkStatus{workspaceCheck("workspace", "."), workspaceCheck("local state directory", state.DirName)}
	stateReadable := true
	if info, err := os.Lstat(filepath.Join(state.DirName, state.FileName)); err == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0222 == 0) {
		stateReadable = false
		checks = append(checks, checkStatus{Name: "local state file", Message: "state must be a writable regular file"})
	} else if err != nil && !os.IsNotExist(err) {
		checks = append(checks, checkStatus{Name: "local state file", Message: "state cannot be inspected"})
	}
	if stateReadable {
		if _, err := state.Load(); err != nil {
			checks = append(checks, checkStatus{Name: "local state", Message: "state is unreadable or malformed"})
		}
	}
	if hasTool(resolved, "docker") {
		checks = append(checks, commandCheck(ctx, timeout, "docker daemon", pathEnv, "docker", "info"))
	}
	if phase != "bootstrap" && phaseNeedsCluster(resolved, phase) && (resolved.Platform.Target == nil || resolved.Platform.Target.Kubernetes == nil) {
		checks = append(checks, commandCheck(ctx, timeout, "kubectl current context", pathEnv, "kubectl", "config", "current-context"))
	}
	return checks
}

// Probe the closest existing parent without creating state/config directories.
func workspaceCheck(name, directory string) checkStatus {
	root, err := os.Getwd()
	if err != nil {
		return checkStatus{Name: name, Message: err.Error()}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return checkStatus{Name: name, Message: err.Error()}
	}
	dir := filepath.Join(root, directory)
	for {
		info, statErr := os.Lstat(dir)
		if os.IsNotExist(statErr) {
			dir = filepath.Dir(dir)
			continue
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0222 == 0 {
			return checkStatus{Name: name, Message: "directory must be writable and must not be a symlink"}
		}
		break
	}
	probe, err := os.CreateTemp(dir, ".platformctl-write-test-*")
	if err != nil {
		return checkStatus{Name: name, Message: err.Error()}
	}
	path := probe.Name()
	err = probe.Close()
	removeErr := os.Remove(path)
	if err != nil || removeErr != nil {
		return checkStatus{Name: name, Message: "could not close/remove workspace probe"}
	}
	return checkStatus{Name: name, OK: true}
}

func commandCheck(ctx context.Context, timeout time.Duration, name, pathEnv, command string, args ...string) checkStatus {
	commandPath, err := lookPathInEnv(pathEnv, command)
	if err == nil {
		_, err = checkexec.Run(ctx, timeout, commandPath, args, appendPath("PATH", pathEnv))
	}
	if err != nil {
		return checkStatus{Name: name, Message: err.Error()}
	}
	return checkStatus{Name: name, OK: true}
}

func lookPathInEnv(pathEnv, command string) (string, error) {
	if pathEnv == "" || filepath.IsAbs(command) || strings.ContainsRune(command, os.PathSeparator) {
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

func preflightFailure(result *preflightResult) error {
	message := "required preflight checks failed"
	for _, check := range result.Checks {
		if !check.OK {
			message = check.Name + ": " + check.Message
			break
		}
	}
	return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_PREFLIGHT_FAILED", message)
}
