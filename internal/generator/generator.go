package generator

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"platformctl/internal/config"
	projecttemplates "platformctl/internal/templates"
)

type Generator struct {
	OutputDir string
}

func New(outputDir string) *Generator {
	return &Generator{OutputDir: outputDir}
}

func (g *Generator) Generate(cfg *config.Config) error {
	dirs := []string{
		filepath.Join(g.OutputDir, "terraform"),
		filepath.Join(g.OutputDir, "kubernetes"),
		filepath.Join(g.OutputDir, "helm-values"),
	}

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove %s: %w", dir, err)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	if err := g.generateTerraform(cfg); err != nil {
		return err
	}
	if err := g.generateKubernetes(cfg); err != nil {
		return err
	}
	if err := g.generateHelmValues(cfg); err != nil {
		return err
	}
	return nil
}

func (g *Generator) renderTemplate(templatePath, outputPath string, data any) error {
	raw, err := projecttemplates.FS.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", templatePath, err)
	}

	tmpl, err := template.New(filepath.Base(templatePath)).Parse(string(raw))
	if err != nil {
		return fmt.Errorf("parse template %s: %w", templatePath, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, data); err != nil {
		return fmt.Errorf("render template %s: %w", templatePath, err)
	}

	if err := os.WriteFile(outputPath, rendered.Bytes(), 0644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}
