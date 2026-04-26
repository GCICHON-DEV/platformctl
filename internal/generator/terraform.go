package generator

import (
	"path/filepath"

	"platformctl/internal/config"
)

func (g *Generator) generateTerraform(cfg *config.Config) error {
	files := map[string]string{
		"terraform/versions.tf.tmpl":  "versions.tf",
		"terraform/variables.tf.tmpl": "variables.tf",
		"terraform/main.tf.tmpl":      "main.tf",
		"terraform/vpc.tf.tmpl":       "vpc.tf",
		"terraform/eks.tf.tmpl":       "eks.tf",
		"terraform/outputs.tf.tmpl":   "outputs.tf",
	}

	for templatePath, fileName := range files {
		out := filepath.Join(g.OutputDir, "terraform", fileName)
		if err := g.renderTemplate(templatePath, out, cfg); err != nil {
			return err
		}
	}

	tfvarsPath := filepath.Join(g.OutputDir, "terraform", "terraform.tfvars")
	return g.renderTemplate("terraform/terraform.tfvars.tmpl", tfvarsPath, cfg)
}
