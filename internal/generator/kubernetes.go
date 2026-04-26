package generator

import (
	"fmt"
	"path/filepath"

	"platformctl/internal/config"
)

func (g *Generator) generateKubernetes(cfg *config.Config) error {
	for _, env := range cfg.Environments {
		base := fmt.Sprintf("%02d-%s", envIndex(cfg.Environments, env.Namespace), env.Namespace)
		files := map[string]string{
			"kubernetes/namespace.yaml.tmpl":     base + "-namespace.yaml",
			"kubernetes/resourcequota.yaml.tmpl": base + "-resourcequota.yaml",
			"kubernetes/limitrange.yaml.tmpl":    base + "-limitrange.yaml",
			"kubernetes/networkpolicy.yaml.tmpl": base + "-networkpolicy.yaml",
		}
		for templatePath, fileName := range files {
			out := filepath.Join(g.OutputDir, "kubernetes", fileName)
			if err := g.renderTemplate(templatePath, out, env); err != nil {
				return err
			}
		}
	}
	return nil
}

func envIndex(envs []config.Environment, namespace string) int {
	for i, env := range envs {
		if env.Namespace == namespace {
			return i + 1
		}
	}
	return 0
}
