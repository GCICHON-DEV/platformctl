package templateengine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"platformctl/internal/executor"

	"gopkg.in/yaml.v3"
)

type PlatformFile struct {
	Template TemplateSource         `yaml:"template"`
	Values   map[string]interface{} `yaml:"values"`
}

type TemplateSource struct {
	Source  string `yaml:"source"`
	Version string `yaml:"version"`
}

type Manifest struct {
	APIVersion   string                 `yaml:"apiVersion"`
	Kind         string                 `yaml:"kind"`
	Name         string                 `yaml:"name"`
	Description  string                 `yaml:"description"`
	Inputs       map[string]Input       `yaml:"inputs"`
	Defaults     map[string]interface{} `yaml:"defaults"`
	Requirements Requirements           `yaml:"requirements"`
	Files        []GeneratedFile        `yaml:"files"`
	Workflow     Workflow               `yaml:"workflow"`
	Outputs      Outputs                `yaml:"outputs"`
}

type Input struct {
	Description string      `yaml:"description"`
	Required    bool        `yaml:"required"`
	Default     interface{} `yaml:"default"`
}

type Requirements struct {
	Tools []Tool `yaml:"tools"`
}

type Tool struct {
	Name        string `yaml:"name"`
	InstallHint string `yaml:"install_hint"`
}

type GeneratedFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

type Workflow struct {
	Up   []Step `yaml:"up"`
	Down []Step `yaml:"down"`
}

type Step struct {
	Name    string   `yaml:"name"`
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
	Dir     string   `yaml:"dir"`
}

type Outputs struct {
	Success []string `yaml:"success"`
}

type Resolved struct {
	Platform PlatformFile
	Manifest Manifest
	Values   map[string]interface{}
	Source   string
}

func Load(platformPath string) (*Resolved, error) {
	platformData, err := os.ReadFile(platformPath)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", platformPath, err)
	}

	var platform PlatformFile
	decoder := yaml.NewDecoder(bytes.NewReader(platformData))
	decoder.KnownFields(true)
	if err := decoder.Decode(&platform); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", platformPath, err)
	}
	if platform.Template.Source == "" {
		return nil, fmt.Errorf("platform.yaml must define template.source")
	}

	resolvedSource, err := ResolveTemplateSource(platform.Template)
	if err != nil {
		return nil, err
	}

	manifestData, err := fetchTemplate(resolvedSource)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	manifestDecoder := yaml.NewDecoder(bytes.NewReader(manifestData))
	manifestDecoder.KnownFields(true)
	if err := manifestDecoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse template from %s: %w", resolvedSource, err)
	}
	if manifest.Name == "" {
		return nil, fmt.Errorf("template from %s must define name", resolvedSource)
	}

	values := mergeValues(manifest, platform.Values)
	return &Resolved{Platform: platform, Manifest: manifest, Values: values, Source: resolvedSource}, nil
}

func ResolveTemplateSource(template TemplateSource) (string, error) {
	source := strings.TrimSpace(template.Source)
	version := strings.TrimSpace(template.Version)

	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		return source, nil
	}
	if version == "" {
		return "", fmt.Errorf("template.version is required when template.source is a registry reference")
	}

	parts := strings.Split(source, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("template.source must be an http(s) URL or registry reference like platformctl/aws-eks-standard")
	}

	switch parts[0] {
	case "platformctl":
		baseURL := strings.TrimRight(os.Getenv("PLATFORMCTL_TEMPLATE_REGISTRY_URL"), "/")
		if baseURL == "" {
			baseURL = "https://raw.githubusercontent.com/GCICHON-DEV/platformctl-templates"
		}
		return fmt.Sprintf("%s/%s/%s.yaml", baseURL, version, parts[1]), nil
	default:
		return "", fmt.Errorf("unsupported template registry namespace %q", parts[0])
	}
}

func (r *Resolved) Validate() error {
	var problems []string
	for key, input := range r.Manifest.Inputs {
		if input.Required && isEmpty(r.Values[key]) {
			problems = append(problems, fmt.Sprintf("values.%s is required", key))
		}
	}
	for key := range r.Platform.Values {
		if _, ok := r.Manifest.Inputs[key]; !ok {
			problems = append(problems, fmt.Sprintf("values.%s is not supported by template %s", key, r.Manifest.Name))
		}
	}
	if len(r.Manifest.Workflow.Up) == 0 {
		problems = append(problems, "template.workflow.up must contain at least one step")
	}
	if len(r.Manifest.Workflow.Down) == 0 {
		problems = append(problems, "template.workflow.down must contain at least one step")
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func (r *Resolved) RequiredTools() []string {
	var tools []string
	seen := map[string]bool{}
	for _, tool := range r.Manifest.Requirements.Tools {
		if tool.Name == "" || seen[tool.Name] {
			continue
		}
		seen[tool.Name] = true
		tools = append(tools, tool.Name)
	}
	return tools
}

func (r *Resolved) CheckTools() []string {
	var missing []string
	for _, tool := range r.RequiredTools() {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}
	return missing
}

func (r *Resolved) Generate() error {
	if err := os.RemoveAll("generated"); err != nil {
		return fmt.Errorf("remove generated: %w", err)
	}
	for _, file := range r.Manifest.Files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return err
		}
		content, err := r.Render(file.Content)
		if err != nil {
			return fmt.Errorf("render %s: %w", file.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(file.Path), 0755); err != nil {
			return fmt.Errorf("create directory for %s: %w", file.Path, err)
		}
		if err := os.WriteFile(file.Path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", file.Path, err)
		}
	}
	return nil
}

func (r *Resolved) RunSteps(runner *executor.Runner, steps []Step) error {
	for _, step := range steps {
		name, err := r.Render(step.Name)
		if err != nil {
			return fmt.Errorf("render step name: %w", err)
		}
		command, err := r.Render(step.Command)
		if err != nil {
			return fmt.Errorf("render command for %s: %w", name, err)
		}
		dir, err := r.Render(step.Dir)
		if err != nil {
			return fmt.Errorf("render dir for %s: %w", name, err)
		}
		args := make([]string, 0, len(step.Args))
		for _, arg := range step.Args {
			rendered, err := r.Render(arg)
			if err != nil {
				return fmt.Errorf("render arg for %s: %w", name, err)
			}
			if rendered == "" {
				continue
			}
			args = append(args, rendered)
		}
		if err := runner.RunStep(name, dir, command, args...); err != nil {
			return err
		}
	}
	return nil
}

func (r *Resolved) Render(text string) (string, error) {
	tmpl, err := template.New("template").Option("missingkey=error").Funcs(template.FuncMap{
		"quote": func(value interface{}) string {
			return fmt.Sprintf("%q", value)
		},
	}).Parse(text)
	if err != nil {
		return "", err
	}

	var out bytes.Buffer
	data := map[string]interface{}{
		"Values": r.Values,
	}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

func (r *Resolved) SuccessMessages() []string {
	var messages []string
	for _, item := range r.Manifest.Outputs.Success {
		rendered, err := r.Render(item)
		if err != nil {
			messages = append(messages, item)
			continue
		}
		messages = append(messages, rendered)
	}
	return messages
}

func mergeValues(manifest Manifest, userValues map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for key, value := range manifest.Defaults {
		out[key] = value
	}
	for key, input := range manifest.Inputs {
		if _, ok := out[key]; !ok && input.Default != nil {
			out[key] = input.Default
		}
	}
	for key, value := range userValues {
		out[key] = value
	}
	return out
}

func isEmpty(value interface{}) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func validateGeneratedPath(path string) error {
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || !strings.HasPrefix(clean, "generated"+string(os.PathSeparator)) {
		return fmt.Errorf("template file path %q must be relative and under generated/", path)
	}
	return nil
}

func fetchTemplate(source string) ([]byte, error) {
	cachePath, err := templateCachePath(source)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(source)
	if err != nil {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("download template %s: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("download template %s: unexpected status %s", source, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read template response %s: %w", source, err)
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return nil, fmt.Errorf("create template cache: %w", err)
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return nil, fmt.Errorf("write template cache: %w", err)
	}

	return data, nil
}

func templateCachePath(source string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory for template cache: %w", err)
	}
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(home, ".platformctl", "templates", hex.EncodeToString(sum[:])+".yaml"), nil
}
