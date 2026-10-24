package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"platformctl/internal/apperror"
)

func TestPlanCommandRendersLocalTemplateAndState(t *testing.T) {
	root := setupCLITemplate(t)
	chdirForTest(t, root)

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
	chdirForTest(t, root)
	t.Setenv("HOME", t.TempDir())

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
	chdirForTest(t, root)
	options.JSON = true
	t.Cleanup(func() { options = cliOptions{} })

	cmd := newStatusCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status command returned error: %v", err)
	}
	var response struct {
		OK    bool                   `json:"ok"`
		State map[string]interface{} `json:"state"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("invalid status JSON: %v: %s", err, out.String())
	}
	if !response.OK || response.State == nil {
		t.Fatalf("status JSON lost ok/state: %s", out.String())
	}
}

func TestStatusJSONPreservesExistingDirectory(t *testing.T) {
	root := t.TempDir()
	writeCLIFile(t, filepath.Join(root, "notes.txt"), "keep these notes\n")
	writeCLIFile(t, filepath.Join(root, "nested", "data.bin"), "\x00\x01\xff\n")
	mkdir(t, filepath.Join(root, "empty"))

	type entry struct {
		Mode os.FileMode
		Data string
	}
	snapshotDirectory := func() map[string]entry {
		t.Helper()
		entries := make(map[string]entry)
		err := filepath.WalkDir(root, func(path string, item os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			info, err := item.Info()
			if err != nil {
				return err
			}
			value := entry{Mode: info.Mode()}
			if !item.IsDir() {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				value.Data = string(data)
			}
			entries[rel] = value
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot directory: %v", err)
		}
		return entries
	}
	before := snapshotDirectory()

	var out, stderr bytes.Buffer
	command := workflowProcess(t, root, "status", "--json")
	command.Stdout = &out
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("status --json failed: %v\n%s", err, stderr.String())
	}
	if after := snapshotDirectory(); !reflect.DeepEqual(before, after) {
		t.Errorf("status --json changed directory: before=%#v after=%#v", before, after)
	}
	var response struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("invalid status JSON: %v\n%s", err, out.String())
	}
	if !response.OK {
		t.Fatalf("status JSON missing ok=true: %s", out.String())
	}
}

func TestPreflightCommandHasDoctorAlias(t *testing.T) {
	cmd := newPreflightCmd()
	if len(cmd.Aliases) != 1 || cmd.Aliases[0] != "doctor" {
		t.Fatalf("preflight aliases = %#v, want doctor", cmd.Aliases)
	}
}

func TestApplyResumeFailsWhenPlanChanges(t *testing.T) {
	root := setupCLITemplate(t)
	chdirForTest(t, root)

	planCmd := newPlanCmd()
	var planOut bytes.Buffer
	planCmd.SetOut(&planOut)
	planCmd.SetErr(&planOut)
	if err := planCmd.Execute(); err != nil {
		t.Fatalf("plan command returned error: %v\n%s", err, planOut.String())
	}

	writeCLIFile(t, filepath.Join(root, "template", "platform.template.yaml"), `
apiVersion: platformctl.io/v1beta1
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
    - name: echo apply changed
      command: echo
      args: ["apply", "{{ .Values.project_name }}", "changed"]
  destroy:
    - name: echo destroy
      command: echo
      args: ["destroy", "{{ .Values.project_name }}"]
`)

	applyCmd := newApplyCmd()
	var out bytes.Buffer
	applyCmd.SetOut(&out)
	applyCmd.SetErr(&out)
	applyCmd.SetArgs([]string{"--yes", "--resume"})
	err := applyCmd.Execute()
	if err == nil {
		t.Fatal("apply --resume succeeded, want error")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %T, want *apperror.Error", err)
	}
	if appErr.Code != "PLATFORMCTL_RESUME_INVALID" {
		t.Fatalf("error code = %q, want PLATFORMCTL_RESUME_INVALID", appErr.Code)
	}
}

func setupCLITemplate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mkdir(t, filepath.Join(root, "template"))
	writeCLIFile(t, filepath.Join(root, "template", "platform.template.yaml"), `
apiVersion: platformctl.io/v1beta1
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

func chdirForTest(t *testing.T, directory string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})
}
