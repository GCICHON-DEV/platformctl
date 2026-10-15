// check-templates validates and renders manifests without installing tools or running steps.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"platformctl/internal/templateengine"

	"gopkg.in/yaml.v3"
)

func check(path string) error {
	data, info, err := templateengine.LoadManifest(templateengine.TemplateSource{Source: path})
	if err != nil {
		return err
	}
	var manifest templateengine.Manifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return err
	}
	values := map[string]interface{}{}
	for key, input := range manifest.Inputs {
		if input.Default == nil && input.Example != nil {
			values[key] = input.Example
		}
	}
	dir, err := os.MkdirTemp("", "platformctl-contract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	previous, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}
	defer os.Chdir(previous)
	config, err := yaml.Marshal(templateengine.PlatformFile{
		Template: templateengine.TemplateSource{Source: info.Resolved}, Values: values,
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile("platform.yaml", config, 0600); err != nil {
		return err
	}
	resolved, err := templateengine.Load("platform.yaml")
	if err != nil {
		return err
	}
	if err := resolved.Validate(); err != nil {
		return err
	}
	if _, err := resolved.BuildPlan(); err != nil {
		return err
	}
	files, err := resolved.Generate()
	if err != nil {
		return err
	}
	if _, err := resolved.GeneratedHash(); err != nil {
		return err
	}
	fmt.Printf("PASS %s: validated and rendered %d files; no workflow executed\n", filepath.Base(path), len(files))
	return nil
}

func main() {
	directory := "examples/template-registry"
	if len(os.Args) == 2 {
		directory = os.Args[1]
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	paths, err := filepath.Glob(filepath.Join(absolute, "*.yaml"))
	if err != nil || len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "no template manifests found")
		os.Exit(1)
	}
	for _, path := range paths {
		if err := check(path); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
	}
}
