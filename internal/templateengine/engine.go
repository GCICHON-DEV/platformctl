package templateengine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"platformctl/internal/apperror"
	"platformctl/internal/executor"

	"gopkg.in/yaml.v3"
)

const (
	DefaultGeneratedDir = "generated"
	LocalManifestName   = "platform.template.yaml"
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
	Metadata     Metadata               `yaml:"metadata"`
	Inputs       map[string]Input       `yaml:"inputs"`
	Defaults     map[string]interface{} `yaml:"defaults"`
	Requirements Requirements           `yaml:"requirements"`
	Files        []GeneratedFile        `yaml:"files"`
	Steps        Steps                  `yaml:"steps"`
	Workflow     Workflow               `yaml:"workflow"` // v1 compatibility.
	Outputs      Outputs                `yaml:"outputs"`
}

type Metadata struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tags        []string `yaml:"tags"`
	Maintainer  string   `yaml:"maintainer"`
}

type Input struct {
	Description string        `yaml:"description"`
	Type        string        `yaml:"type"`
	Required    bool          `yaml:"required"`
	Default     interface{}   `yaml:"default"`
	Example     interface{}   `yaml:"example"`
	Sensitive   bool          `yaml:"sensitive"`
	Enum        []interface{} `yaml:"enum"`
	Validation  string        `yaml:"validation"`
}

type Requirements struct {
	Tools       []Tool       `yaml:"tools"`
	Credentials []Credential `yaml:"credentials"`
	Warnings    []string     `yaml:"warnings"`
}

type Tool struct {
	Name        string   `yaml:"name" json:"name"`
	Version     string   `yaml:"version" json:"version,omitempty"`
	InstallHint string   `yaml:"install_hint" json:"install_hint,omitempty"`
	MinVersion  string   `yaml:"min_version" json:"min_version,omitempty"`
	VersionArgs []string `yaml:"version_args" json:"version_args,omitempty"`
}

type Credential struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description,omitempty"`
	Command     string `yaml:"command" json:"-"`
}

type GeneratedFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
	Source  string `yaml:"source"`
}

type Steps struct {
	Plan    []Step `yaml:"plan"`
	Apply   []Step `yaml:"apply"`
	Destroy []Step `yaml:"destroy"`
}

type Workflow struct {
	Up   []Step `yaml:"up"`
	Down []Step `yaml:"down"`
}

type Step struct {
	ID             string   `yaml:"id"`
	Name           string   `yaml:"name"`
	Command        string   `yaml:"command"`
	Args           []string `yaml:"args"`
	Dir            string   `yaml:"dir"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	Suggestion     string   `yaml:"suggestion"`
	Preflight      bool     `yaml:"preflight"`
	Retry          Retry    `yaml:"retry"`
}

type Retry struct {
	Attempts     int `yaml:"attempts" json:"attempts,omitempty"`
	DelaySeconds int `yaml:"delay_seconds" json:"delay_seconds,omitempty"`
}

type Outputs struct {
	Success   []string `yaml:"success"`
	Notes     []string `yaml:"notes"`
	NextSteps []string `yaml:"next_steps"`
}

type SourceInfo struct {
	Original string `json:"original,omitempty"`
	Resolved string `json:"resolved"`
	Version  string `json:"version,omitempty"`
	Kind     string `json:"kind"`
	BaseDir  string `json:"base_dir,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

type Resolved struct {
	Platform PlatformFile
	Manifest Manifest
	Values   map[string]interface{}
	Source   string
	Info     SourceInfo
}

type ExecutionPlan struct {
	TemplateName    string        `json:"template_name"`
	TemplateSource  SourceInfo    `json:"template_source"`
	GeneratedFiles  []string      `json:"generated_files"`
	RequiredTools   []Tool        `json:"required_tools,omitempty"`
	Credentials     []Credential  `json:"credentials,omitempty"`
	Warnings        []string      `json:"warnings,omitempty"`
	PlanSteps       []PlannedStep `json:"plan_steps,omitempty"`
	ApplySteps      []PlannedStep `json:"apply_steps"`
	DestroySteps    []PlannedStep `json:"destroy_steps"`
	SuccessMessages []string      `json:"success_messages,omitempty"`
}

type PlannedStep struct {
	ID             string   `json:"id"`
	Index          int      `json:"index"`
	Name           string   `json:"name"`
	Command        string   `json:"command"`
	Args           []string `json:"args,omitempty"`
	Dir            string   `json:"dir"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	Suggestion     string   `json:"suggestion,omitempty"`
	Preflight      bool     `json:"preflight,omitempty"`
	Retry          *Retry   `json:"retry,omitempty"`
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
	if platform.Values == nil {
		platform.Values = map[string]interface{}{}
	}

	manifestData, info, err := LoadManifest(platform.Template)
	if err != nil {
		return nil, err
	}

	var manifest Manifest
	manifestDecoder := yaml.NewDecoder(bytes.NewReader(manifestData))
	manifestDecoder.KnownFields(true)
	if err := manifestDecoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse template from %s: %w", info.Resolved, err)
	}
	normalizeManifest(&manifest)
	if manifest.Name == "" {
		return nil, fmt.Errorf("template from %s must define name or metadata.name", info.Resolved)
	}
	normalizePlatformValues(&platform, manifest)

	values := mergeValues(manifest, platform.Values)
	return &Resolved{
		Platform: platform,
		Manifest: manifest,
		Values:   values,
		Source:   info.Resolved,
		Info:     info,
	}, nil
}

func LoadManifest(template TemplateSource) ([]byte, SourceInfo, error) {
	resolved, err := ResolveTemplateSource(template)
	if err != nil {
		return nil, SourceInfo{}, err
	}
	data, err := fetchTemplate(resolved)
	if err != nil {
		return nil, SourceInfo{}, err
	}
	resolved.Checksum = checksum(data)
	return data, resolved, nil
}

func ResolveTemplateSource(template TemplateSource) (SourceInfo, error) {
	source := strings.TrimSpace(template.Source)
	version := strings.TrimSpace(template.Version)
	if source == "" {
		return SourceInfo{}, fmt.Errorf("template.source is required")
	}
	if inlineSource, inlineVersion, ok := strings.Cut(source, "@"); ok {
		source = inlineSource
		if version == "" {
			version = inlineVersion
		}
	}

	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		return SourceInfo{Original: template.Source, Resolved: source, Version: version, Kind: "url"}, nil
	}

	if isLocalSource(source) {
		var err error
		source, err = expandLocalSource(source)
		if err != nil {
			return SourceInfo{}, err
		}
		clean := filepath.Clean(source)
		stat, err := os.Stat(clean)
		if err != nil {
			return SourceInfo{}, fmt.Errorf("read local template source %s: %w", source, err)
		}
		if stat.IsDir() {
			return SourceInfo{
				Original: template.Source,
				Resolved: filepath.Join(clean, LocalManifestName),
				Version:  version,
				Kind:     "local",
				BaseDir:  clean,
			}, nil
		}
		return SourceInfo{
			Original: template.Source,
			Resolved: clean,
			Version:  version,
			Kind:     "local",
			BaseDir:  filepath.Dir(clean),
		}, nil
	}

	if version == "" {
		return SourceInfo{}, fmt.Errorf("template.version is required when template.source is a registry reference")
	}

	parts := strings.Split(source, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return SourceInfo{}, fmt.Errorf("template.source must be an http(s) URL, local path, or registry reference like platformctl/aws-eks-standard")
	}

	switch parts[0] {
	case "platformctl":
		baseURL := strings.TrimRight(os.Getenv("PLATFORMCTL_TEMPLATE_REGISTRY_URL"), "/")
		if baseURL == "" {
			baseURL = "https://raw.githubusercontent.com/GCICHON-DEV/platformctl-templates"
		}
		return SourceInfo{
			Original: template.Source,
			Resolved: fmt.Sprintf("%s/%s/%s.yaml", baseURL, version, parts[1]),
			Version:  version,
			Kind:     "registry",
		}, nil
	default:
		return SourceInfo{}, fmt.Errorf("unsupported template registry namespace %q", parts[0])
	}
}

func (r *Resolved) Validate() error {
	var problems []string
	if r.Manifest.APIVersion != "platformctl.io/v1alpha2" {
		if r.Manifest.APIVersion == "" {
			problems = append(problems, "template.apiVersion is required")
		} else {
			problems = append(problems, fmt.Sprintf("template.apiVersion %q is not supported; expected platformctl.io/v1alpha2", r.Manifest.APIVersion))
		}
	}
	if r.Manifest.Kind != "" && r.Manifest.Kind != "PlatformTemplate" {
		problems = append(problems, "template.kind must be PlatformTemplate")
	}
	if strings.TrimSpace(r.Manifest.Metadata.Name) == "" && strings.TrimSpace(r.Manifest.Name) == "" {
		problems = append(problems, "template.metadata.name is required")
	}
	if strings.TrimSpace(r.Manifest.Description) == "" && strings.TrimSpace(r.Manifest.Metadata.Description) == "" {
		problems = append(problems, "template.metadata.description is required")
	}
	for key, input := range r.Manifest.Inputs {
		if strings.TrimSpace(input.Description) == "" {
			problems = append(problems, fmt.Sprintf("inputs.%s.description is required", key))
		}
		value := r.Values[key]
		if input.Required && isEmpty(value) {
			problems = append(problems, fmt.Sprintf("values.%s is required", key))
			continue
		}
		if isEmpty(value) {
			continue
		}
		if err := validateInputValue(key, input, value); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for key := range r.Platform.Values {
		if _, ok := r.Manifest.Inputs[key]; !ok {
			problems = append(problems, fmt.Sprintf("values.%s is not supported by template %s", key, r.Manifest.Name))
		}
	}
	if len(r.StepsFor("apply")) == 0 {
		problems = append(problems, "template.steps.apply must contain at least one step")
	}
	if len(r.StepsFor("destroy")) == 0 {
		problems = append(problems, "template.steps.destroy must contain at least one step")
	}
	for _, file := range r.Manifest.Files {
		if err := validateGeneratedPath(file.Path); err != nil {
			problems = append(problems, err.Error())
		}
		if file.Content == "" && file.Source == "" {
			problems = append(problems, fmt.Sprintf("template file %s must define content or source", file.Path))
		}
	}
	for _, phase := range []string{"plan", "apply", "destroy"} {
		seenStepIDs := map[string]bool{}
		for i, step := range r.StepsFor(phase) {
			if strings.TrimSpace(step.Name) == "" {
				problems = append(problems, fmt.Sprintf("template.steps.%s[%d].name is required", phase, i))
			}
			if strings.TrimSpace(step.Command) == "" {
				problems = append(problems, fmt.Sprintf("template.steps.%s[%d].command is required", phase, i))
			}
			if step.TimeoutSeconds < 0 {
				problems = append(problems, fmt.Sprintf("template.steps.%s[%d].timeout_seconds cannot be negative", phase, i))
			}
			if step.Retry.Attempts < 0 || step.Retry.DelaySeconds < 0 {
				problems = append(problems, fmt.Sprintf("template.steps.%s[%d].retry values cannot be negative", phase, i))
			}
			if err := validateStepSafety(phase, i, step); err != nil {
				problems = append(problems, err.Error())
			}
			id := strings.TrimSpace(step.ID)
			if id == "" {
				id = strings.TrimSpace(step.Name)
			}
			id = stableStepName(id)
			if seenStepIDs[id] {
				problems = append(problems, fmt.Sprintf("template.steps.%s[%d] duplicates step id %q", phase, i, id))
			}
			seenStepIDs[id] = true
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid configuration:\n- %s", strings.Join(problems, "\n- "))
	}
	return nil
}

func (r *Resolved) RequiredTools() []string {
	tools := make([]string, 0, len(r.Manifest.Requirements.Tools))
	seen := map[string]bool{}
	for _, tool := range r.Manifest.Requirements.Tools {
		if tool.Name == "" || seen[tool.Name] {
			continue
		}
		seen[tool.Name] = true
		tools = append(tools, tool.Name)
	}
	sort.Strings(tools)
	return tools
}

func (r *Resolved) RequirementTools() []Tool {
	seen := map[string]bool{}
	var tools []Tool
	for _, tool := range r.Manifest.Requirements.Tools {
		if tool.Name == "" || seen[tool.Name] {
			continue
		}
		seen[tool.Name] = true
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
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

func (r *Resolved) BuildPlan() (*ExecutionPlan, error) {
	generated, err := r.RenderedFilePaths()
	if err != nil {
		return nil, err
	}
	planSteps, err := r.plannedSteps("plan")
	if err != nil {
		return nil, err
	}
	applySteps, err := r.plannedSteps("apply")
	if err != nil {
		return nil, err
	}
	destroySteps, err := r.plannedSteps("destroy")
	if err != nil {
		return nil, err
	}
	return &ExecutionPlan{
		TemplateName:    r.Manifest.Name,
		TemplateSource:  r.Info,
		GeneratedFiles:  generated,
		RequiredTools:   r.RequirementTools(),
		Credentials:     r.Manifest.Requirements.Credentials,
		Warnings:        r.Manifest.Requirements.Warnings,
		PlanSteps:       planSteps,
		ApplySteps:      applySteps,
		DestroySteps:    destroySteps,
		SuccessMessages: r.SuccessMessages(),
	}, nil
}

func (r *Resolved) RenderedFilePaths() ([]string, error) {
	paths := make([]string, 0, len(r.Manifest.Files))
	for _, file := range r.Manifest.Files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return nil, err
		}
		paths = append(paths, filepath.Clean(file.Path))
	}
	sort.Strings(paths)
	return paths, nil
}

func (r *Resolved) Generate() ([]string, error) {
	if err := os.RemoveAll(DefaultGeneratedDir); err != nil {
		return nil, fmt.Errorf("remove %s: %w", DefaultGeneratedDir, err)
	}
	written := make([]string, 0, len(r.Manifest.Files))
	for _, file := range r.Manifest.Files {
		if err := validateGeneratedPath(file.Path); err != nil {
			return nil, err
		}
		content, err := r.renderGeneratedFile(file)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", file.Path, err)
		}
		if err := os.MkdirAll(filepath.Dir(file.Path), 0755); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", file.Path, err)
		}
		if err := os.WriteFile(file.Path, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", file.Path, err)
		}
		written = append(written, filepath.Clean(file.Path))
	}
	sort.Strings(written)
	return written, nil
}

func (r *Resolved) StepsFor(phase string) []Step {
	switch phase {
	case "plan":
		return r.Manifest.Steps.Plan
	case "apply":
		if len(r.Manifest.Steps.Apply) > 0 {
			return r.Manifest.Steps.Apply
		}
		return r.Manifest.Workflow.Up
	case "destroy":
		if len(r.Manifest.Steps.Destroy) > 0 {
			return r.Manifest.Steps.Destroy
		}
		return r.Manifest.Workflow.Down
	default:
		return nil
	}
}

func (r *Resolved) RunSteps(runner *executor.Runner, phase string, skipCompleted map[string]bool, markCompleted func(string) error) error {
	steps := r.StepsFor(phase)
	for i, step := range steps {
		planned, err := r.planStep(i, step)
		if err != nil {
			return err
		}
		stepID := StepID(phase, planned.Index, planned.Name)
		if planned.ID != "" {
			stepID = StepID(phase, planned.Index, planned.ID)
		}
		if skipCompleted[stepID] {
			fmt.Fprintf(runner.Stdout, "\n==> skipping completed step %s\n", stepID)
			continue
		}
		err = runPlannedStep(runner, phase, stepID, planned)
		if err != nil {
			return apperror.WithRemediation(
				apperror.WithContext(
					apperror.Wrap(err, apperror.CategoryExecution, "PLATFORMCTL_STEP_FAILED", fmt.Sprintf("workflow step %s failed", stepID)),
					fmt.Sprintf("step=%s name=%q dir=%q command=%q", stepID, planned.Name, planned.Dir, executor.MaskCommand(planned.Command, planned.Args)),
				),
				resumeSuggestion(phase, planned.Suggestion),
			)
		}
		if markCompleted != nil {
			if err := markCompleted(stepID); err != nil {
				return err
			}
		}
	}
	return nil
}

func runPlannedStep(runner *executor.Runner, phase, stepID string, planned PlannedStep) error {
	attempts := 1
	if planned.Retry != nil {
		attempts = planned.Retry.Attempts
	}
	if attempts < 1 {
		attempts = 1
	}
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			fmt.Fprintf(runner.Stdout, "\n==> retrying %s (%d/%d)\n", stepID, attempt, attempts)
			if planned.Retry != nil && planned.Retry.DelaySeconds > 0 {
				time.Sleep(time.Duration(planned.Retry.DelaySeconds) * time.Second)
			}
		}
		err = runner.RunStepWithOptions(executor.StepOptions{
			ID:             stepID,
			Name:           planned.Name,
			Dir:            planned.Dir,
			Command:        planned.Command,
			Args:           planned.Args,
			TimeoutSeconds: planned.TimeoutSeconds,
			Suggestion:     planned.Suggestion,
			ResumeCommand:  "platformctl " + phase + " --resume",
		})
		if err == nil {
			return nil
		}
	}
	return err
}

func StepID(phase string, index int, name string) string {
	cleanName := stableStepName(name)
	return fmt.Sprintf("%s:%02d:%s", phase, index+1, cleanName)
}

func stableStepName(name string) string {
	cleanName := strings.ToLower(strings.TrimSpace(name))
	cleanName = strings.Join(strings.Fields(cleanName), "-")
	cleanName = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(cleanName, "-")
	cleanName = strings.Trim(cleanName, "-")
	if cleanName == "" {
		return "step"
	}
	return cleanName
}

func (r *Resolved) Render(text string) (string, error) {
	tmpl, err := template.New("template").Option("missingkey=error").Funcs(template.FuncMap{
		"quote": func(value interface{}) string {
			return fmt.Sprintf("%q", value)
		},
		"default": func(fallback, value interface{}) interface{} {
			if isEmpty(value) {
				return fallback
			}
			return value
		},
	}).Parse(text)
	if err != nil {
		return "", fmt.Errorf("parse Go template expression: %w", err)
	}

	var out bytes.Buffer
	data := map[string]interface{}{
		"Values": r.Values,
	}
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("execute Go template expression: %w", err)
	}
	return out.String(), nil
}

func (r *Resolved) SuccessMessages() []string {
	return r.renderMessages(r.Manifest.Outputs.Success)
}

func (r *Resolved) NoteMessages() []string {
	return r.renderMessages(r.Manifest.Outputs.Notes)
}

func (r *Resolved) NextStepMessages() []string {
	return r.renderMessages(r.Manifest.Outputs.NextSteps)
}

func (r *Resolved) renderMessages(items []string) []string {
	messages := make([]string, 0, len(items))
	for _, item := range items {
		rendered, err := r.Render(item)
		if err != nil {
			messages = append(messages, item)
			continue
		}
		messages = append(messages, rendered)
	}
	return messages
}

func (r *Resolved) GeneratedHash() (string, error) {
	paths, err := r.RenderedFilePaths()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		b.WriteString(path)
		b.WriteByte(0)
		b.Write(data)
		b.WriteByte(0)
	}
	return checksum([]byte(b.String())), nil
}

func (r *Resolved) plannedSteps(phase string) ([]PlannedStep, error) {
	steps := r.StepsFor(phase)
	out := make([]PlannedStep, 0, len(steps))
	for i, step := range steps {
		planned, err := r.planStep(i, step)
		if err != nil {
			return nil, err
		}
		out = append(out, planned)
	}
	return out, nil
}

func (r *Resolved) planStep(index int, step Step) (PlannedStep, error) {
	name, err := r.Render(step.Name)
	if err != nil {
		return PlannedStep{}, fmt.Errorf("render step name: %w", err)
	}
	command, err := r.Render(step.Command)
	if err != nil {
		return PlannedStep{}, fmt.Errorf("render command for %s: %w", name, err)
	}
	dir, err := r.Render(step.Dir)
	if err != nil {
		return PlannedStep{}, fmt.Errorf("render dir for %s: %w", name, err)
	}
	args := make([]string, 0, len(step.Args))
	for _, arg := range step.Args {
		rendered, err := r.Render(arg)
		if err != nil {
			return PlannedStep{}, fmt.Errorf("render arg for %s: %w", name, err)
		}
		if rendered == "" {
			continue
		}
		args = append(args, rendered)
	}
	suggestion, err := r.Render(step.Suggestion)
	if err != nil {
		return PlannedStep{}, fmt.Errorf("render suggestion for %s: %w", name, err)
	}
	stepNameForID := name
	if strings.TrimSpace(step.ID) != "" {
		stepNameForID = step.ID
	}
	return PlannedStep{
		ID:             stableStepName(stepNameForID),
		Index:          index,
		Name:           name,
		Command:        command,
		Args:           args,
		Dir:            filepath.Clean(dir),
		TimeoutSeconds: step.TimeoutSeconds,
		Suggestion:     suggestion,
		Preflight:      step.Preflight,
		Retry:          plannedRetry(step.Retry),
	}, nil
}

func plannedRetry(retry Retry) *Retry {
	if retry.Attempts == 0 && retry.DelaySeconds == 0 {
		return nil
	}
	return &retry
}

func resumeSuggestion(phase, stepSuggestion string) string {
	var parts []string
	if strings.TrimSpace(stepSuggestion) != "" {
		parts = append(parts, strings.TrimSpace(stepSuggestion))
	}
	if phase == "apply" || phase == "destroy" {
		parts = append(parts, fmt.Sprintf("After fixing the issue, run platformctl %s --resume.", phase))
	}
	return strings.Join(parts, " ")
}

func (r *Resolved) renderGeneratedFile(file GeneratedFile) (string, error) {
	raw := file.Content
	if file.Source != "" {
		if r.Info.BaseDir == "" {
			return "", fmt.Errorf("source files are supported only for local template directories")
		}
		cleanSource := filepath.Clean(file.Source)
		if filepath.IsAbs(cleanSource) || strings.HasPrefix(cleanSource, "..") {
			return "", fmt.Errorf("template file source %q must be relative to template directory", file.Source)
		}
		data, err := os.ReadFile(filepath.Join(r.Info.BaseDir, cleanSource))
		if err != nil {
			return "", err
		}
		raw = string(data)
	}
	return r.Render(raw)
}

func normalizeManifest(manifest *Manifest) {
	if manifest.Name == "" {
		manifest.Name = manifest.Metadata.Name
	}
	if manifest.Description == "" {
		manifest.Description = manifest.Metadata.Description
	}
	if manifest.Inputs == nil {
		manifest.Inputs = map[string]Input{}
	}
	if manifest.Defaults == nil {
		manifest.Defaults = map[string]interface{}{}
	}
	if len(manifest.Steps.Apply) == 0 && len(manifest.Workflow.Up) > 0 {
		manifest.Steps.Apply = manifest.Workflow.Up
	}
	if len(manifest.Steps.Destroy) == 0 && len(manifest.Workflow.Down) > 0 {
		manifest.Steps.Destroy = manifest.Workflow.Down
	}
}

func normalizePlatformValues(platform *PlatformFile, manifest Manifest) {
	if platform.Values == nil {
		platform.Values = map[string]interface{}{}
	}
	_, hasRegionInput := manifest.Inputs["region"]
	_, hasAWSRegionInput := manifest.Inputs["aws_region"]
	if hasAWSRegionInput && !hasRegionInput {
		if value, ok := platform.Values["region"]; ok {
			platform.Values["aws_region"] = value
			delete(platform.Values, "region")
		}
	}
	if hasRegionInput && !hasAWSRegionInput {
		if value, ok := platform.Values["aws_region"]; ok {
			platform.Values["region"] = value
			delete(platform.Values, "aws_region")
		}
	}
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

func validateInputValue(key string, input Input, value interface{}) error {
	if input.Type != "" {
		if err := validateType(key, input.Type, value); err != nil {
			return err
		}
	}
	if len(input.Enum) > 0 {
		matched := false
		for _, item := range input.Enum {
			if reflect.DeepEqual(fmt.Sprint(item), fmt.Sprint(value)) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("values.%s must be one of: %s", key, joinInterface(input.Enum))
		}
	}
	if input.Validation != "" {
		text := fmt.Sprint(value)
		matched, err := regexp.MatchString(input.Validation, text)
		if err != nil {
			return fmt.Errorf("inputs.%s.validation is invalid: %w", key, err)
		}
		if !matched {
			return fmt.Errorf("values.%s does not match validation %q", key, input.Validation)
		}
	}
	return nil
}

func validateStepSafety(phase string, index int, step Step) error {
	command := strings.TrimSpace(step.Command)
	if strings.ContainsAny(command, " \t\n\r;&|`$<>") {
		return fmt.Errorf("template.steps.%s[%d].command must be a single executable name, not a shell expression", phase, index)
	}
	for argIndex, arg := range step.Args {
		if strings.ContainsAny(arg, "\x00\n\r") {
			return fmt.Errorf("template.steps.%s[%d].args[%d] contains an unsafe control character", phase, index, argIndex)
		}
	}
	return nil
}

func validateType(key, typ string, value interface{}) error {
	switch typ {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("values.%s must be a string", key)
		}
	case "number", "integer":
		switch value.(type) {
		case int, int64, float64, float32:
			return nil
		default:
			if _, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
				return nil
			}
			return fmt.Errorf("values.%s must be an integer", key)
		}
	case "bool", "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("values.%s must be a boolean", key)
		}
	case "array", "list":
		if reflect.ValueOf(value).Kind() != reflect.Slice {
			return fmt.Errorf("values.%s must be an array", key)
		}
	case "object", "map":
		if reflect.ValueOf(value).Kind() != reflect.Map {
			return fmt.Errorf("values.%s must be an object", key)
		}
	default:
		return fmt.Errorf("inputs.%s.type %q is not supported", key, typ)
	}
	return nil
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
	if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || !strings.HasPrefix(clean, DefaultGeneratedDir+string(os.PathSeparator)) {
		return fmt.Errorf("template file path %q must be relative and under %s/", path, DefaultGeneratedDir)
	}
	return nil
}

func fetchTemplate(source SourceInfo) ([]byte, error) {
	if source.Kind == "local" {
		return os.ReadFile(source.Resolved)
	}
	if !strings.HasPrefix(source.Resolved, "https://") && !strings.HasPrefix(source.Resolved, "http://") {
		return nil, fmt.Errorf("template.source must resolve to an http(s) URL or local path")
	}

	cachePath, err := templateCachePath(source.Resolved)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(source.Resolved)
	if err != nil {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("download template %s: %w", source.Resolved, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("download template %s: unexpected status %s", source.Resolved, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read template response %s: %w", source.Resolved, err)
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

func isLocalSource(source string) bool {
	return strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") || strings.HasPrefix(source, "~")
}

func expandLocalSource(source string) (string, error) {
	if source == "~" || strings.HasPrefix(source, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate home directory for template source: %w", err)
		}
		if source == "~" {
			return home, nil
		}
		return filepath.Join(home, strings.TrimPrefix(source, "~/")), nil
	}
	return source, nil
}

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func joinInterface(items []interface{}) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprint(item))
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

var ErrNoPlan = errors.New("execution plan is empty")
