package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"platformctl/internal/state"
)

// Execute the real command implementation in a separate OS process. No mocks,
// cloud tools, managed tool downloads, or user kubeconfig are involved.
func TestWorkflowProcess(t *testing.T) {
	if os.Getenv("PLATFORMCTL_TEST_PROCESS") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	os.Exit(runCLI(rootCmd, args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func workflowProcess(t *testing.T, root string, args ...string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestWorkflowProcess$", "--"}, args...)...)
	command.Dir = root
	command.Env = append(os.Environ(), "PLATFORMCTL_TEST_PROCESS=1", "HOME="+root)
	return command
}

func waitFor(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", description)
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, name := range []string{"platform.yaml", "generated/out.txt", ".platformctl/state.json"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		result[name] = string(data)
	}
	return result
}

func blockingTemplate(t *testing.T) string {
	root := setupCLITemplate(t)
	writeCLIFile(t, filepath.Join(root, "template", "platform.template.yaml"), `
apiVersion: platformctl.io/v1beta1
kind: PlatformTemplate
metadata:
  name: lock-test
  description: Local locking fixture.
inputs:
  project_name:
    description: Fixture project.
    type: string
files:
  - path: generated/out.txt
    content: protected output
steps:
  apply:
    - name: checkpoint
      command: echo
      args: [done]
    - name: wait
      command: sh
      args: [-c, 'touch ready; i=0; while [ ! -f proceed ]; do i=$((i+1)); [ "$i" -lt 500 ] || exit 1; sleep 0.02; done; touch finished']
  destroy:
    - name: checkpoint
      command: echo
      args: [done]
    - name: wait
      command: sh
      args: [-c, 'touch ready; i=0; while [ ! -f proceed ]; do i=$((i+1)); [ "$i" -lt 500 ] || exit 1; sleep 0.02; done; touch finished']
`)
	return root
}

func TestWorkflowsRejectConcurrentMutations(t *testing.T) {
	for _, phase := range []string{"apply", "destroy"} {
		t.Run(phase, func(t *testing.T) {
			root := blockingTemplate(t)
			holder := workflowProcess(t, root, phase, "--yes")
			var output bytes.Buffer
			holder.Stdout = &output
			holder.Stderr = &output
			if err := holder.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = holder.Process.Kill()
				_ = os.WriteFile(filepath.Join(root, "proceed"), nil, 0644)
				deadline := time.Now().Add(time.Second)
				for !exists(filepath.Join(root, "finished")) && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				_ = holder.Wait()
			})
			waitFor(t, "running second step", func() bool { return exists(filepath.Join(root, "ready")) })
			before := snapshot(t, root)
			var checkpoint state.State
			if err := json.Unmarshal([]byte(before[".platformctl/state.json"]), &checkpoint); err != nil {
				t.Fatal(err)
			}
			if len(checkpoint.CompletedSteps) != 1 {
				t.Fatalf("expected first checkpoint: %+v", checkpoint)
			}
			for _, args := range [][]string{{"plan"}, {"init", "--force", "--template", "./template"}, {"apply", "--yes"}, {"destroy", "--yes"}, {"up", "--yes"}, {"down", "--yes"}, {"plan", "--json"}} {
				data, err := workflowProcess(t, root, args...).CombinedOutput()
				if err == nil || !strings.Contains(string(data), "another platformctl workflow") {
					t.Fatalf("%v bypassed lock: %v %s", args, err, data)
				}
				after := snapshot(t, root)
				for name, want := range before {
					if after[name] != want {
						t.Fatalf("%v modified %s/checkpoints", args, name)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(root, "proceed"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			if err := holder.Wait(); err != nil {
				t.Fatalf("holder failed: %v %s", err, output.String())
			}
			after := snapshot(t, root)
			if err := json.Unmarshal([]byte(after[".platformctl/state.json"]), &checkpoint); err != nil {
				t.Fatal(err)
			}
			if len(checkpoint.CompletedSteps) != 2 || checkpoint.LastPhase != phase {
				t.Fatalf("lost checkpoint: %+v", checkpoint)
			}
			if data, err := workflowProcess(t, root, "plan").CombinedOutput(); err != nil {
				t.Fatalf("plan after release: %v %s", err, data)
			}
		})
	}
}

func TestInitAndPlanLockBeforeReadingTemplate(t *testing.T) {
	for _, phase := range []string{"init", "plan"} {
		t.Run(phase, func(t *testing.T) {
			root := setupCLITemplate(t)
			manifest := filepath.Join(root, "template", "platform.template.yaml")
			if err := os.Remove(manifest); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(manifest, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{phase}
			if phase == "init" {
				args = append(args, "--force", "--template", "./template")
			}
			holder := workflowProcess(t, root, args...)
			if err := holder.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = holder.Process.Kill(); _ = holder.Wait() })
			waitFor(t, "lock before blocked template read", func() bool {
				data, _ := os.ReadFile(filepath.Join(root, state.LockPath()))
				return strings.Contains(string(data), "operation="+phase)
			})
			for _, other := range []string{"apply", "destroy", "plan", "init"} {
				data, err := workflowProcess(t, root, other).CombinedOutput()
				if err == nil || !strings.Contains(string(data), "another platformctl workflow") {
					t.Fatalf("%s bypassed %s lock: %v %s", other, phase, err, data)
				}
			}
			if exists(filepath.Join(root, state.Path())) || exists(filepath.Join(root, "generated")) {
				t.Fatal("contender wrote state or generated files")
			}
		})
	}
}

func TestCrashRetainsLockUntilSurvivingStepExits(t *testing.T) {
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL} {
		t.Run(signal.String(), func(t *testing.T) {
			root := blockingTemplate(t)
			holder := workflowProcess(t, root, "apply", "--yes")
			if err := holder.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = holder.Process.Kill()
				_ = os.WriteFile(filepath.Join(root, "proceed"), nil, 0644)
				deadline := time.Now().Add(time.Second)
				for !exists(filepath.Join(root, "finished")) && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				_ = holder.Wait()
			})
			waitFor(t, "surviving step", func() bool { return exists(filepath.Join(root, "ready")) })
			before := snapshot(t, root)
			if err := holder.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			_ = holder.Wait()
			data, err := workflowProcess(t, root, "plan").CombinedOutput()
			if err == nil || !strings.Contains(string(data), "another platformctl workflow") {
				t.Fatalf("crash unlocked running step: %v %s", err, data)
			}
			after := snapshot(t, root)
			for name, want := range before {
				if after[name] != want {
					t.Fatalf("crash contender modified %s", name)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "proceed"), nil, 0644); err != nil {
				t.Fatal(err)
			}
			// Kernel drops the final descriptor after the orphan finishes. The CLI did
			// not checkpoint that step: resume must retain the first completed step.
			waitFor(t, "orphan to close inherited lock", func() bool {
				data, err = workflowProcess(t, root, "apply", "--yes", "--resume").CombinedOutput()
				return err == nil
			})
			var checkpoint state.State
			data, err = os.ReadFile(filepath.Join(root, state.Path()))
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &checkpoint); err != nil {
				t.Fatal(err)
			}
			if len(checkpoint.CompletedSteps) != 2 {
				t.Fatalf("resume lost checkpoints: %s", data)
			}

		})
	}
}
