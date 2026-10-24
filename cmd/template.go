package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"platformctl/internal/apperror"
	"platformctl/internal/templateengine"
)

func newTemplateCmd() *cobra.Command {
	parent := &cobra.Command{Use: "template", Short: "Explicitly trust or update a remote template digest"}
	for _, update := range []bool{false, true} {
		update := update
		name := "lock"
		if update {
			name = "update"
		}
		var source, version string
		command := &cobra.Command{Use: name, Short: "Download and explicitly approve the current remote manifest bytes", Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return usageFailure(err)
			}
			return nil
		}, RunE: func(cmd *cobra.Command, args []string) error {
			if options.Offline {
				return fmt.Errorf("trust/update requires online review; --offline cannot update a lock")
			}
			lease, err := acquireWorkflowLock("template-" + name)
			if err != nil {
				return err
			}
			defer lease.Release()
			ref := templateengine.TemplateSource{Source: source, Version: version}
			if source == "" {
				ref, err = templateengine.ReadTemplateReference(defaultConfigFile)
				if err != nil {
					return err
				}
			}
			result, err := templateengine.TrustSource(ref, update)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_TRUST", "template trust operation failed")
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]interface{}{"ok": true, "source": result.Source, "sha256": result.SHA256, "lock": templateengine.SourceLockPath})
			}
			printf(cmd, "Trusted template %s\nSHA256: %s\nLock: %s\n", result.Source, result.SHA256, templateengine.SourceLockPath)
			return nil
		}}
		command.Flags().StringVar(&source, "source", "", "remote URL or registry source (otherwise read platform.yaml)")
		command.Flags().StringVar(&version, "version", "", "explicit registry ref for --source")
		parent.AddCommand(command)
	}
	return parent
}
