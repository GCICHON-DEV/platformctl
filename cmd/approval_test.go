package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"platformctl/internal/templateengine"
)

// Compare actual generated/state/toolchain bytes, including absent files. Lock
// metadata is diagnostic and is deliberately excluded (the inode stays locked).
func approvalFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, dir := range []string{"generated", ".platformctl", filepath.Join(".home", ".platformctl")} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() || path == filepath.Join(root, ".platformctl", "lock") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			result[rel] = string(data)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func runApproval(t *testing.T, root string, input string, args ...string) (string, error) {
	t.Helper()
	command := workflowProcess(t, root, args...)
	// Keep the managed toolchain distinct from the workspace lock/state.
	command.Env = append(command.Env, "HOME="+filepath.Join(root, ".home"))
	command.Stdin = strings.NewReader(input)
	data, err := command.CombinedOutput()
	return string(data), err
}

func mustApproval(t *testing.T, root string, args ...string) string {
	t.Helper()
	output, err := runApproval(t, root, "", args...)
	if err != nil {
		t.Fatalf("%v: %v\n%s", args, err, output)
	}
	return output
}

func TestApprovalRejectionPreservesFilesAndDoesNotInstall(t *testing.T) {
	for _, phase := range []string{"apply", "destroy"} {
		for _, existing := range []bool{false, true} {
			for _, mode := range []string{"cancel", "json", "eof"} {
				t.Run(phase+"/"+mode+"/existing="+map[bool]string{false: "no", true: "yes"}[existing], func(t *testing.T) {
					root := setupCLITemplate(t)
					// A pinned managed tool would attempt installation if Ensure ran.
					path := filepath.Join(root, "template", "platform.template.yaml")
					data, _ := os.ReadFile(path)
					writeCLIFile(t, path, strings.Replace(string(data), "- name: echo", "- name: terraform\n      version: 1.9.8", 1))
					if existing {
						mustApproval(t, root, "plan")
					}
					before := approvalFiles(t, root)
					args := []string{phase}
					input := "n\n"
					if mode == "json" {
						args = append(args, "--json")
						input = ""
					}
					if mode == "eof" {
						input = ""
					}
					out, err := runApproval(t, root, input, args...)
					if mode == "cancel" && (err != nil || !strings.Contains(out, "cancelled")) {
						t.Fatalf("cancel: %v %s", err, out)
					}
					if mode != "cancel" && err == nil {
						t.Fatalf("%s unexpectedly succeeded: %s", mode, out)
					}
					if mode == "json" && !strings.Contains(out, "PLATFORMCTL_CONFIRMATION_REQUIRED") {
						t.Fatal(out)
					}
					if after := approvalFiles(t, root); !reflect.DeepEqual(before, after) {
						t.Fatalf("rejection changed files: before=%v after=%v", before, after)
					}
					if exists(filepath.Join(root, ".home", ".platformctl", "bin")) {
						t.Fatal("rejection initialized managed tools")
					}
				})
			}
		}
	}
}

func TestApprovedPreviewRejectsDriftBeforeMutation(t *testing.T) {
	for _, phase := range []string{"apply", "destroy"} {
		for _, resume := range []bool{false, true} {
			for _, change := range []string{"inline", "source-file", "source-ref", "input", "unused-input", "args", "target-env", "target-file", "docker-config", "explicit-kubeconfig", "generated", "missing"} {
				t.Run(phase+"/"+map[bool]string{false: "apply-again", true: "resume"}[resume]+"/"+change, func(t *testing.T) {
					root := setupCLITemplate(t)
					manifestPath := filepath.Join(root, "template", "platform.template.yaml")
					data, _ := os.ReadFile(manifestPath)
					if change == "explicit-kubeconfig" {
						data = bytes.ReplaceAll(data, []byte(`args: [`), []byte(`args: ["--kubeconfig", "target.yaml", `))
						writeCLIFile(t, manifestPath, string(data))
						writeCLIFile(t, filepath.Join(root, "target.yaml"), "current-context: first\n")
					}
					if change == "unused-input" {
						data = bytes.Replace(data, []byte("inputs:\n"), []byte("inputs:\n  target:\n    description: Unused target selector.\n    type: string\n    default: first\n"), 1)
						writeCLIFile(t, manifestPath, string(data))
					}
					if change == "source-file" {
						writeCLIFile(t, manifestPath, strings.Replace(string(data), `content: "hello {{ .Values.project_name }}"`, `source: content.txt`, 1))
						writeCLIFile(t, filepath.Join(root, "template", "content.txt"), "hello {{ .Values.project_name }}")
					}
					mustApproval(t, root, phase, "--yes")
					switch change {
					case "inline":
						writeCLIFile(t, manifestPath, strings.Replace(string(data), "hello ", "changed ", 1))
					case "source-file":
						writeCLIFile(t, filepath.Join(root, "template", "content.txt"), "changed {{ .Values.project_name }}")
					case "source-ref":
						writeCLIFile(t, filepath.Join(root, "other", "platform.template.yaml"), string(data))
						writeCLIFile(t, filepath.Join(root, "platform.yaml"), "template:\n  source: ./other\nvalues:\n  project_name: demo\n")
					case "input":
						writeCLIFile(t, filepath.Join(root, "platform.yaml"), "template:\n  source: ./template\nvalues:\n  project_name: another\n")
					case "unused-input":
						writeCLIFile(t, filepath.Join(root, "platform.yaml"), "template:\n  source: ./template\nvalues:\n  project_name: demo\n  target: second\n")
					case "args":
						writeCLIFile(t, manifestPath, strings.ReplaceAll(string(data), `args: [`, `args: ["changed", `))
					case "target-file":
						writeCLIFile(t, filepath.Join(root, ".home", ".kube", "config"), "current-context: another\n")
					case "docker-config":
						writeCLIFile(t, filepath.Join(root, ".home", ".docker", "config.json"), `{"currentContext":"changed"}`)
					case "explicit-kubeconfig":
						writeCLIFile(t, filepath.Join(root, "target.yaml"), "current-context: changed\n")
					case "generated":
						writeCLIFile(t, filepath.Join(root, "generated", "out.txt"), "manually edited")
					case "missing":
						if err := os.Remove(filepath.Join(root, "generated", "out.txt")); err != nil {
							t.Fatal(err)
						}
					}
					before := approvalFiles(t, root)
					args := []string{phase, "--yes"}
					if resume {
						args = append(args, "--resume")
					}
					command := workflowProcess(t, root, args...)
					command.Env = append(command.Env, "HOME="+filepath.Join(root, ".home"))
					if change == "target-env" {
						command.Env = append(command.Env, "AWS_PROFILE=changed-target")
					}
					out, err := command.CombinedOutput()
					if err == nil || !strings.Contains(string(out), "PLATFORMCTL_") {
						t.Fatalf("drift allowed: %v %s", err, out)
					}
					if after := approvalFiles(t, root); !reflect.DeepEqual(before, after) {
						t.Fatalf("drift rejection mutated files: before=%v after=%v", before, after)
					}
				})
			}
		}
	}
}

func TestPreviewIsNotApprovalAndResumeRequiresSamePhase(t *testing.T) {
	root := setupCLITemplate(t)
	mustApproval(t, root, "plan")
	for _, phase := range []string{"apply", "destroy"} {
		out, err := runApproval(t, root, "", phase, "--yes", "--resume")
		if err == nil || !strings.Contains(out, "PLATFORMCTL_RESUME_INVALID") {
			t.Fatalf("preview authorized resume: %v %s", err, out)
		}
	}
	mustApproval(t, root, "apply", "--yes")
	out := mustApproval(t, root, "apply", "--yes", "--resume")
	if !strings.Contains(out, "skipping completed step") {
		t.Fatal(out)
	}
	if out, err := runApproval(t, root, "", "destroy", "--yes", "--resume"); err == nil || !strings.Contains(out, "PLATFORMCTL_RESUME_INVALID") {
		t.Fatalf("wrong phase resume: %v %s", err, out)
	}
	mustApproval(t, root, "destroy", "--yes")
	mustApproval(t, root, "destroy", "--yes", "--resume")
}

func TestLegacyApprovalFailsClosedAndPlanMigrates(t *testing.T) {
	root := setupCLITemplate(t)
	mustApproval(t, root, "apply", "--yes")
	path := filepath.Join(root, ".platformctl", "state.json")
	data, _ := os.ReadFile(path)
	var st map[string]interface{}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	delete(st, "plan_format")
	delete(st, "approved_plan_hash")
	delete(st, "approved_phase")
	data, _ = json.Marshal(st)
	writeCLIFile(t, path, string(data))
	before := approvalFiles(t, root)
	for _, args := range [][]string{{"apply", "--yes"}, {"apply", "--yes", "--resume"}} {
		if out, err := runApproval(t, root, "", args...); err == nil {
			t.Fatalf("legacy accepted: %s", out)
		}
		if !reflect.DeepEqual(before, approvalFiles(t, root)) {
			t.Fatal("legacy rejection changed files")
		}
	}
	mustApproval(t, root, "plan")
	mustApproval(t, root, "apply", "--yes")
}

type approvalReader func([]byte) (int, error)

func (f approvalReader) Read(p []byte) (int, error) { return f(p) }

func TestApprovalRechecksSourceAfterPrompt(t *testing.T) {
	root := setupCLITemplate(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	t.Setenv("HOME", filepath.Join(root, ".home"))
	before := approvalFiles(t, root)
	command := newApplyCmd()
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetIn(approvalReader(func(p []byte) (int, error) {
		path := filepath.Join(root, "template", "platform.template.yaml")
		data, _ := os.ReadFile(path)
		writeCLIFile(t, path, strings.Replace(string(data), "hello ", "changed ", 1))
		return copy(p, "yes\n"), nil
	}))
	err = command.Execute()
	if err == nil || !strings.Contains(err.Error(), "preview changed") {
		t.Fatalf("source change during approval: %v %s", err, output.String())
	}
	if !reflect.DeepEqual(before, approvalFiles(t, root)) {
		t.Fatal("changed preview mutated files")
	}
}

func TestSnapshotWritesCapturedSourceBytes(t *testing.T) {
	root := setupCLITemplate(t)
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)
	resolved, err := templateengine.Load("platform.yaml")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := resolved.BuildPlan()
	if err != nil {
		t.Fatal(err)
	}
	// Mutation of the in-memory template after approval cannot alter execution bytes.
	resolved.Manifest.Files[0].Content = "different"
	if _, err := plan.GenerateWithPrevious(nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("generated/out.txt")
	if err != nil || string(data) != "hello demo" {
		t.Fatalf("snapshot changed: %q %v", data, err)
	}
}

func TestTargetDriftDuringWorkflowStopsBeforeNextStep(t *testing.T) {
	root := setupCLITemplate(t)
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, _ := os.ReadFile(path)
	old := `    - name: echo apply
      command: echo
      args: ["apply", "{{ .Values.project_name }}"]`
	replacement := `    - name: change target
      command: sh
      args: [-c, 'mkdir -p "$HOME/.kube"; echo "current-context: changed" > "$HOME/.kube/config"']
    - name: must not run
      command: touch
      args: [unexpected-step]`
	writeCLIFile(t, path, strings.Replace(string(data), old, replacement, 1))
	out, err := runApproval(t, root, "", "apply", "--yes")
	if err == nil || !strings.Contains(out, "PLATFORMCTL_PLAN_CHANGED") {
		t.Fatalf("target drift accepted: %v %s", err, out)
	}
	if exists(filepath.Join(root, "unexpected-step")) {
		t.Fatal("step ran against changed target")
	}
	stateBytes, _ := os.ReadFile(filepath.Join(root, ".platformctl", "state.json"))
	var st struct {
		CompletedSteps map[string]bool `json:"completed_steps"`
	}
	if err := json.Unmarshal(stateBytes, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.CompletedSteps) != 1 {
		t.Fatalf("checkpoint not preserved: %s", stateBytes)
	}
}

func TestDeclaredGeneratedTargetIsStableAcrossPreviewAndApply(t *testing.T) {
	root := setupCLITemplate(t)
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, _ := os.ReadFile(path)
	writeCLIFile(t, path, strings.ReplaceAll(string(data), `args: [`, `args: ["--kubeconfig", "generated/out.txt", `))
	mustApproval(t, root, "plan")
	mustApproval(t, root, "apply", "--yes")
	mustApproval(t, root, "apply", "--yes", "--resume")
}

func TestTargetDriftPreventsRetryAgainstChangedContext(t *testing.T) {
	root := setupCLITemplate(t)
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, _ := os.ReadFile(path)
	old := `    - name: echo apply
      command: echo
      args: ["apply", "{{ .Values.project_name }}"]`
	replacement := `    - name: failing target changer
      command: sh
      args: [-c, 'if [ -f attempted ]; then touch unexpected-retry; fi; touch attempted; mkdir -p "$HOME/.kube"; echo "current-context: changed" > "$HOME/.kube/config"; exit 1']
      retry:
        attempts: 2`
	writeCLIFile(t, path, strings.Replace(string(data), old, replacement, 1))
	out, err := runApproval(t, root, "", "apply", "--yes")
	if err == nil {
		t.Fatalf("unexpected success: %s", out)
	}
	if !exists(filepath.Join(root, "attempted")) || exists(filepath.Join(root, "unexpected-retry")) {
		t.Fatal("retry ran against a changed target")
	}
}

func TestRelativeTargetEnvironmentUsesStepDirectory(t *testing.T) {
	for _, phase := range []string{"apply", "destroy"} {
		for _, selection := range []string{"KUBECONFIG", "AWS_CONFIG_FILE", "AWS_SHARED_CREDENTIALS_FILE", "DOCKER_CONFIG"} {
			t.Run(phase+"/"+selection, func(t *testing.T) {
				root := setupCLITemplate(t)
				path := filepath.Join(root, "template", "platform.template.yaml")
				data, _ := os.ReadFile(path)
				// The root-level decoy must not hide changes to the effective target.
				configPath := "target.yaml"
				if selection == "DOCKER_CONFIG" {
					configPath = "target.yaml/config.json"
				}
				writeCLIFile(t, filepath.Join(root, configPath), "root configuration")
				actual := filepath.Join(root, "generated", "work", configPath)
				writeCLIFile(t, actual, "first target")
				writeCLIFile(t, path, strings.ReplaceAll(string(data), "      command: echo", "      dir: generated/work\n      command: echo"))
				t.Setenv(selection, "target.yaml")
				mustApproval(t, root, "plan")
				mustApproval(t, root, phase, "--yes")
				writeCLIFile(t, actual, "different effective target")
				before := approvalFiles(t, root)
				for _, extra := range [][]string{nil, {"--resume"}} {
					args := append([]string{phase, "--yes"}, extra...)
					out, err := runApproval(t, root, "", args...)
					if err == nil || (!strings.Contains(out, "PLATFORMCTL_PLAN_CHANGED") && !strings.Contains(out, "PLATFORMCTL_RESUME_INVALID")) {
						t.Fatalf("relative target drift accepted: %v %s", err, out)
					}
					if !reflect.DeepEqual(before, approvalFiles(t, root)) {
						t.Fatal("target rejection changed files")
					}
				}
			})
		}
	}

}

func TestTerraformChdirBindsEffectiveWorkspace(t *testing.T) {
	for _, phase := range []string{"apply", "destroy"} {
		for _, dataDir := range []string{"", "relative-data"} {
			t.Run(phase+"/"+dataDir, func(t *testing.T) {
				root := setupCLITemplate(t)
				// Local executable fixture, never real Terraform or cloud operations.
				shimDir := filepath.Join(root, "tools")
				shim := filepath.Join(shimDir, "terraform")
				writeCLIFile(t, shim, "#!/bin/sh\nexit 0\n")
				if err := os.Chmod(shim, 0755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", shimDir+string(os.PathListSeparator)+os.Getenv("PATH"))
				t.Setenv("TF_DATA_DIR", dataDir)
				selected := dataDir
				if selected == "" {
					selected = ".terraform"
				}
				actual := filepath.Join(root, "generated", "work", "module", selected, "environment")
				writeCLIFile(t, actual, "first")
				// Decoy directory that the initial implementation incorrectly hashed.
				writeCLIFile(t, filepath.Join(root, "generated", "work", selected, "environment"), "unchanged")
				path := filepath.Join(root, "template", "platform.template.yaml")
				data, _ := os.ReadFile(path)
				changed := strings.ReplaceAll(string(data), "      command: echo", "      dir: generated/work\n      command: terraform")
				changed = strings.ReplaceAll(changed, `args: [`, `args: ["-chdir=module", `)
				writeCLIFile(t, path, changed)
				mustApproval(t, root, "plan")
				mustApproval(t, root, phase, "--yes")
				writeCLIFile(t, actual, "changed-target-workspace")
				before := approvalFiles(t, root)
				for _, extra := range [][]string{nil, {"--resume"}} {
					args := append([]string{phase, "--yes"}, extra...)
					out, err := runApproval(t, root, "", args...)
					if err == nil || (!strings.Contains(out, "PLATFORMCTL_PLAN_CHANGED") && !strings.Contains(out, "PLATFORMCTL_RESUME_INVALID")) {
						t.Fatalf("Terraform -chdir drift accepted: %v %s", err, out)
					}
					if !reflect.DeepEqual(before, approvalFiles(t, root)) {
						t.Fatal("rejection mutated files")
					}
				}
			})
		}
	}
}
