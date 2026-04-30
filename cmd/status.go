package cmd

import (
	"sort"

	"platformctl/internal/apperror"
	"platformctl/internal/state"

	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show local platformctl state",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl status
  platformctl status --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := state.Load()
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryState, "PLATFORMCTL_STATE_LOAD", "could not load local state")
			}
			if options.JSON {
				writeJSON(cmd.OutOrStdout(), map[string]interface{}{"ok": true, "state": st})
				return nil
			}
			if st.TemplateSource == "" {
				println(cmd, "No local platform state found.")
				return nil
			}
			printf(cmd, "Template:       %s\n", st.TemplateSource)
			if st.TemplateVersion != "" {
				printf(cmd, "Version:        %s\n", st.TemplateVersion)
			}
			printf(cmd, "Checksum:       %s\n", st.TemplateChecksum)
			printf(cmd, "Generated hash: %s\n", st.GeneratedHash)
			printf(cmd, "Last phase:     %s\n", st.LastPhase)
			printf(cmd, "Updated:        %s\n", st.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
			if len(st.CompletedSteps) > 0 {
				keys := make([]string, 0, len(st.CompletedSteps))
				for key := range st.CompletedSteps {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				println(cmd, "Completed steps:")
				for _, key := range keys {
					printf(cmd, "  - %s\n", key)
				}
			}
			return nil
		},
	}
}
