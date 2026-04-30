package cmd

import (
	"fmt"
	"os"
	"strings"

	"platformctl/internal/apperror"
	"platformctl/internal/deps"
	"platformctl/internal/templateengine"
	"platformctl/internal/toolchain"

	"github.com/spf13/cobra"
)

func ensureToolchain(cmd *cobra.Command, resolved *templateengine.Resolved, strict bool) (*toolchain.Manager, []deps.Status, error) {
	manager, err := toolchain.New(workflowStdout(cmd.OutOrStdout(), cmd.ErrOrStderr()), cmd.ErrOrStderr())
	if err != nil {
		return nil, nil, apperror.Wrap(err, apperror.CategoryDependency, "PLATFORMCTL_TOOLCHAIN_INIT", "could not initialize managed toolchain")
	}
	statuses, err := manager.Ensure(resolved.RequirementTools())
	if err != nil {
		return nil, nil, apperror.Wrap(err, apperror.CategoryDependency, "PLATFORMCTL_TOOLCHAIN_ENSURE", "could not verify or install managed tools")
	}
	if len(statuses) > 0 {
		printDependencyStatus(cmd, statuses)
	}
	if strict {
		if missing := deps.Missing(statuses); len(missing) > 0 {
			return manager, statuses, apperror.WithRemediation(
				apperror.New(apperror.CategoryDependency, "PLATFORMCTL_MISSING_TOOLS", fmt.Sprintf("%d required dependencies are missing", len(missing))),
				"Run platformctl preflight for details, then install the missing tools or use a template with managed versions.",
			)
		}
		for _, status := range statuses {
			if problem := deps.VersionProblem(status); problem != "" {
				return manager, statuses, apperror.WithRemediation(
					apperror.New(apperror.CategoryDependency, "PLATFORMCTL_VERSION_MISMATCH", problem),
					"Install the pinned version or update the template requirements.",
				)
			}
		}
	}
	return manager, statuses, nil
}

func appendPath(key, value string) []string {
	env := os.Environ()
	prefix := key + "="
	for i, item := range env {
		if strings.HasPrefix(item, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
