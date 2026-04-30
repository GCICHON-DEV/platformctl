package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCommandRendersLocalTemplateAndState(t *testing.T) {
	root := setupCLITemplate(t)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cmd := newPlanCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("plan command returned error: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Plan rendered") {
		t.Fatalf("plan output missing success message: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "generated", "out.txt")); err != nil {
		t.Fatalf("generated file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".platformctl", "state.json")); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestApplyCommandRunsEchoTemplateAndUpAliasExists(t *testing.T) {
	root := setupCLITemplate(t)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	cmd := newApplyCmd()
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "up" {
		t.Fatalf("apply aliases = %#v, want up", cmd.Aliases)
	}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply command returned error: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Apply workflow finished") {
		t.Fatalf("apply output missing success message: %s", out.String())
	}
}

func TestStatusCommandSupportsJSON(t *testing.T) {
	root := setupCLITemplate(t)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	options.JSON = true
	t.Cleanup(func() { options = cliOptions{} })

	cmd := newStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command returned error: %v", err)
	}
	if !strings.Contains(out.String(), `"ok": true`) {
		t.Fatalf("status JSON missing ok=true: %s", out.String())
	}
}

func TestPreflightCommandHasDoctorAlias(t *testing.T) {
	cmd := newPreflightCmd()
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "doctor" {
		t.Fatalf("preflight aliases = %#v, want doctor", cmd.Aliases)
	}
}

func setupCLITemplate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "template"))
	writeCLIFile(t, filepath.Join(root, "template", "platform.template.yaml"), `
apiVersion: platformctl.io/v1alpha2
kind: PlatformTemplate
metadata:
  name: cli-test
  description: CLI test template.
inputs:
  project_name:
    description: Project name for CLI tests.
    type: string
    required: true
requirements:
  tools:
    - name: echo
files:
  - path: generated/out.txt
    content: "hello {{ .Values.project_name }}"
steps:
  apply:
    - name: echo apply
      command: echo
      args: ["apply", "{{ .Values.project_name }}"]
  destroy:
    - name: echo destroy
      command: echo
      args: ["destroy", "{{ .Values.project_name }}"]
`)
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), `
template:
  source: ./template
values:
  project_name: demo
`)
	return root
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func writeCLIFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimLeft(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
}
