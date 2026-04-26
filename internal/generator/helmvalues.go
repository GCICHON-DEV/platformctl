package generator

import (
	"path/filepath"

	"platformctl/internal/config"
)

func (g *Generator) generateHelmValues(cfg *config.Config) error {
	files := map[string]string{
		"helm-values/argocd-values.yaml.tmpl":                "argocd-values.yaml",
		"helm-values/kube-prometheus-stack-values.yaml.tmpl": "kube-prometheus-stack-values.yaml",
		"helm-values/ingress-nginx-values.yaml.tmpl":         "ingress-nginx-values.yaml",
	}
	for templatePath, fileName := range files {
		out := filepath.Join(g.OutputDir, "helm-values", fileName)
		if err := g.renderTemplate(templatePath, out, cfg); err != nil {
			return err
		}
	}
	return nil
}
