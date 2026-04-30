package cmd

import (
	"fmt"
	"os"
	"strings"

	"platformctl/internal/apperror"
	"platformctl/internal/templateengine"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newInitCmd() *cobra.Command {
	var templateSource string
	var version string
	var projectName string
	var force bool

	cmd := &cobra.Command{
		Use:           "init",
		Short:         "Create a platform.yaml for a platform template",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: `  platformctl init --template platformctl/local-kind-standard --project demo
  platformctl init --template platformctl/aws-eks-standard --version v1.0.0 --project demo
  platformctl init --template ./examples/local-templates/custom-minimal --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if templateSource == "" {
				templateSource = "platformctl/local-kind-standard"
			}
			if version == "" && !strings.HasPrefix(templateSource, ".") && !strings.HasPrefix(templateSource, "/") && !strings.HasPrefix(templateSource, "http") && !strings.Contains(templateSource, "@") {
				version = "v1.0.0"
			}
			if projectName == "" {
				projectName = "demo"
			}
			if _, err := os.Stat(defaultConfigFile); err == nil && !force {
				return apperror.New(apperror.CategoryConfig, "PLATFORMCTL_CONFIG_EXISTS", fmt.Sprintf("%s already exists; use --force to overwrite", defaultConfigFile))
			} else if err != nil && !os.IsNotExist(err) {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_STAT", "could not inspect platform.yaml")
			}

			values := map[string]interface{}{
				"project_name": projectName,
			}
			if strings.Contains(templateSource, "aws-eks") {
				values["region"] = "eu-central-1"
				values["aws_profile"] = "default"
				values["cluster_name"] = projectName + "-platform"
			}
			if strings.Contains(templateSource, "local-kind") {
				values["cluster_name"] = projectName + "-local"
			}
			doc := map[string]interface{}{
				"template": map[string]interface{}{
					"source": templateSource,
				},
				"values": values,
			}
			if version != "" {
				doc["template"].(map[string]interface{})["version"] = version
			}
			data, err := yaml.Marshal(doc)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryInternal, "PLATFORMCTL_CONFIG_ENCODE", "could not encode platform.yaml")
			}
			if err := os.WriteFile(defaultConfigFile, data, 0644); err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_WRITE", "could not write platform.yaml")
			}
			printf(cmd, "Created %s using template %s.\n", defaultConfigFile, templateSource)
			resolved, err := templateengine.Load(defaultConfigFile)
			if err != nil {
				return apperror.Wrap(err, apperror.CategoryConfig, "PLATFORMCTL_CONFIG_LOAD", "could not load platform.yaml")
			}
			if err := resolved.Validate(); err != nil {
				return apperror.Wrap(err, apperror.CategoryTemplate, "PLATFORMCTL_TEMPLATE_INVALID", "template validation failed")
			}
			println(cmd, "\nPreflight:")
			result, err := runPreflight(cmd, resolved, false)
			if err != nil {
				return err
			}
			if result.OK {
				println(cmd, "\nInit complete. Run platformctl plan next.")
			} else {
				println(cmd, "\nInit complete with warnings. Fix the reported items before platformctl apply.")
			}
			if options.JSON {
				writeJSON(cmd.OutOrStdout(), map[string]interface{}{"ok": result.OK, "config": defaultConfigFile, "template": templateSource, "preflight": result})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&templateSource, "template", "", "template source, for example platformctl/local-kind-standard or ./templates/company-platform")
	cmd.Flags().StringVar(&version, "version", "", "template version for registry sources")
	cmd.Flags().StringVar(&projectName, "project", "", "project name")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite platform.yaml if it already exists")
	return cmd
}
