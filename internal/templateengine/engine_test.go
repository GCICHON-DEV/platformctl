package templateengine

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolveTemplateSourceRegistryWithInlineVersion(t *testing.T) {
	t.Setenv("PLATFORMCTL_TEMPLATE_REGISTRY_URL", "https://example.test/templates")

	info, err := ResolveTemplateSource(TemplateSource{Source: "platformctl/local-kind-standard@v1.0.0"})
	if err != nil {
		t.Fatalf("ResolveTemplateSource returned error: %v", err)
	}
	if info.Kind != "registry" {
		t.Fatalf("kind = %q, want registry", info.Kind)
	}
	if info.Version != "v1.0.0" {
		t.Fatalf("version = %q, want v1.0.0", info.Version)
	}
	want := "https://example.test/templates/v1.0.0/local-kind-standard.yaml"
	if info.Resolved != want {
		t.Fatalf("resolved = %q, want %q", info.Resolved, want)
	}
}

func TestLoadLocalTemplateDirectoryAndGenerateSourceFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "template", "files"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "template", LocalManifestName), `
apiVersion: platformctl.io/v1alpha2
kind: PlatformTemplate
metadata:
  name: local-test
  description: Local test template.
inputs:
  project_name:
    description: Project name for the local test template.
    type: string
    required: true
requirements:
  tools:
    - name: echo
files:
  - path: generated/out.txt
    source: files/out.txt.tmpl
steps:
  apply:
    - name: say hello
      command: echo
      args: ["hello", "{{ .Values.project_name }}"]
  destroy:
    - name: say bye
      command: echo
      args: ["bye"]
`)
	writeFile(t, filepath.Join(root, "template", "files", "out.txt.tmpl"), "project={{ .Values.project_name }}\n")
	writeFile(t, filepath.Join(root, "platform.yaml"), `
template:
  source: ./template
values:
  project_name: demo
`)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	resolved, err := Load("platform.yaml")
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if err := resolved.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	written, err := resolved.Generate()
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if len(written) != 1 || written[0] != filepath.Join("generated", "out.txt") {
		t.Fatalf("written = %#v", written)
	}
	data, err := os.ReadFile(filepath.Join(root, "generated", "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "project=demo\n" {
		t.Fatalf("generated content = %q", string(data))
	}
}

func TestValidateRejectsUnsupportedValuesAndUnsafeGeneratedPath(t *testing.T) {
	resolved := &Resolved{
		Manifest: Manifest{
			APIVersion: "platformctl.io/v1alpha2",
			Name:       "bad",
			Inputs: map[string]Input{
				"project_name": {Type: "string", Required: true},
			},
			Files: []GeneratedFile{
				{Path: "../escape.txt", Content: "bad"},
			},
			Steps: Steps{
				Apply:   []Step{{Name: "apply", Command: "echo"}},
				Destroy: []Step{{Name: "destroy", Command: "echo"}},
			},
		},
		Platform: PlatformFile{Values: map[string]interface{}{"project_name": "demo", "extra": "nope"}},
		Values:   map[string]interface{}{"project_name": "demo", "extra": "nope"},
	}

	err := resolved.Validate()
	if err == nil {
		t.Fatal("Validate succeeded, want error")
	}
	text := err.Error()
	if !strings.Contains(text, "values.extra is not supported") {
		t.Fatalf("missing unsupported value error: %s", text)
	}
	if !strings.Contains(text, "must be relative and under generated/") {
		t.Fatalf("missing unsafe path error: %s", text)
	}
}

func TestBuildPlanDoesNotExecuteCommands(t *testing.T) {
	resolved := &Resolved{
		Manifest: Manifest{
			APIVersion: "platformctl.io/v1alpha2",
			Name:       "plan-only",
			Inputs:     map[string]Input{},
			Files:      []GeneratedFile{{Path: "generated/out.txt", Content: "ok"}},
			Steps: Steps{
				Plan:    []Step{{Name: "must not run", Command: "definitely-not-a-real-command"}},
				Apply:   []Step{{Name: "apply", Command: "echo"}},
				Destroy: []Step{{Name: "destroy", Command: "echo"}},
			},
		},
		Values: map[string]interface{}{},
	}

	plan, err := resolved.BuildPlan()
	if err != nil {
		t.Fatalf("BuildPlan returned error: %v", err)
	}
	if len(plan.PlanSteps) != 1 {
		t.Fatalf("plan steps = %d, want 1", len(plan.PlanSteps))
	}
	if plan.PlanSteps[0].Command != "definitely-not-a-real-command" {
		t.Fatalf("command = %q", plan.PlanSteps[0].Command)
	}
}

func TestValidateRequiresInputDescriptionAndSafeStepCommand(t *testing.T) {
	resolved := &Resolved{
		Manifest: Manifest{
			APIVersion: "platformctl.io/v1alpha2",
			Metadata: Metadata{
				Name:        "unsafe",
				Description: "Unsafe command test.",
			},
			Inputs: map[string]Input{
				"project_name": {Type: "string", Required: true},
			},
			Files: []GeneratedFile{{Path: "generated/out.txt", Content: "ok"}},
			Steps: Steps{
				Apply: []Step{
					{Name: "apply one", Command: "echo ok"},
					{Name: "apply one", Command: "echo"},
				},
				Destroy: []Step{{Name: "destroy", Command: "echo"}},
			},
		},
		Platform: PlatformFile{Values: map[string]interface{}{"project_name": "demo"}},
		Values:   map[string]interface{}{"project_name": "demo"},
	}

	err := resolved.Validate()
	if err == nil {
		t.Fatal("Validate succeeded, want error")
	}
	text := err.Error()
	for _, want := range []string{
		"inputs.project_name.description is required",
		"command must be a single executable name",
		"duplicates step id",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("validation error missing %q:\n%s", want, text)
		}
	}
}

func TestPlanStepUsesExplicitStableID(t *testing.T) {
	resolved := &Resolved{Values: map[string]interface{}{}}
	planned, err := resolved.planStep(0, Step{ID: "Install Monitoring", Name: "install", Command: "echo"})
	if err != nil {
		t.Fatalf("planStep returned error: %v", err)
	}
	if planned.ID != "install-monitoring" {
		t.Fatalf("planned ID = %q, want install-monitoring", planned.ID)
	}
}

func TestExampleTemplateManifestsValidate(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test file")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "examples", "template-registry")
	for _, name := range []string{"aws-eks-standard.yaml", "local-kind-standard.yaml", "azure-aks-standard.yaml"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(root, name))
			if err != nil {
				t.Fatal(err)
			}
			var manifest Manifest
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			decoder.KnownFields(true)
			if err := decoder.Decode(&manifest); err != nil {
				t.Fatalf("decode manifest: %v", err)
			}
			normalizeManifest(&manifest)
			values := map[string]interface{}{}
			for key, input := range manifest.Inputs {
				if input.Default != nil {
					values[key] = input.Default
					continue
				}
				if input.Example != nil {
					values[key] = input.Example
				}
			}
			resolved := &Resolved{Manifest: manifest, Platform: PlatformFile{Values: values}, Values: mergeValues(manifest, values)}
			if err := resolved.Validate(); err != nil {
				t.Fatalf("Validate returned error: %v", err)
			}
			if _, err := resolved.BuildPlan(); err != nil {
				t.Fatalf("BuildPlan returned error: %v", err)
			}
		})
	}
}

func TestStepIDIsStable(t *testing.T) {
	got := StepID("apply", 2, "Install Monitoring")
	want := "apply:03:install-monitoring"
	if got != want {
		t.Fatalf("StepID = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
}
