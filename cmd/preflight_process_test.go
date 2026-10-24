package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreflightProcessBlocksMutation(t *testing.T) {
	for _, problem := range []string{"credential", "account", "context", "readonly"} {
		for _, phase := range []string{"apply", "destroy"} {
			t.Run(problem+"/"+phase, func(t *testing.T) {
				root := setupCLITemplate(t)
				bin := filepath.Join(root, "bin")
				mkdir(t, bin)
				manifestPath := filepath.Join(root, "template", "platform.template.yaml")
				data, err := os.ReadFile(manifestPath)
				if err != nil {
					t.Fatal(err)
				}
				manifest := strings.ReplaceAll(string(data), "command: echo", "command: sh")
				manifest = strings.ReplaceAll(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, `args: [-c, 'touch mutated']`)
				manifest = strings.ReplaceAll(manifest, `args: ["destroy", "{{ .Values.project_name }}"]`, `args: [-c, 'touch mutated']`)
				config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				switch problem {
				case "credential":
					manifest = strings.Replace(manifest, "requirements:", "requirements:\n  credentials:\n    - name: synthetic\n      command: false", 1)
				case "account":
					checkStub(t, bin, "aws", `case "$1" in version|--version) echo 'aws-cli/2.0.0';; sts) printf '{"Account":"999999999999"}';; configure) echo eu-central-1;; *) exit 2;; esac`)
					manifest = strings.Replace(manifest, "  tools:", "  tools:\n    - name: aws", 1)
					config = append(config, []byte("target:\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n")...)
				case "context":
					checkStub(t, bin, "kubectl", `case "$1" in version|--version) echo 'kubectl 1.35.0';; config) printf '{"current-context":"foreign"}';; *) touch mutated;; esac`)
					manifest = strings.Replace(manifest, "  tools:", "  tools:\n    - name: kubectl", 1)
					config = append(config, []byte("target:\n  kubernetes:\n    context: expected\n    cluster: expected\n    server: https://cluster.invalid\n")...)
				case "readonly":
					mkdir(t, filepath.Join(root, ".platformctl"))
					if err := os.Chmod(root, 0500); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Chmod(root, 0700) })
				}
				// Write fixture before restricting the root's directory entries.
				if problem == "readonly" {
					_ = os.Chmod(root, 0700)
				}
				writeCLIFile(t, manifestPath, manifest)
				writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config))
				if problem == "readonly" {
					_ = os.Chmod(root, 0500)
				}
				before := approvalFiles(t, root)
				process := workflowProcess(t, root, phase, "--yes", "--json")
				process.Env = append(process.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AWS_REGION=eu-central-1", "AWS_DEFAULT_REGION=eu-central-1")
				out, err := process.CombinedOutput()
				if err == nil || !bytes.Contains(out, []byte(`"ok": false`)) {
					t.Fatalf("accepted %s: %v %s", problem, err, out)
				}
				if exists(filepath.Join(root, "mutated")) || exists(filepath.Join(root, "generated")) {
					t.Fatalf("mutation after refused preflight: %s", out)
				}
				after := approvalFiles(t, root)
				if len(before) != len(after) {
					t.Fatalf("state changed: before=%v after=%v", before, after)
				}
				for path, value := range before {
					if after[path] != value {
						t.Fatalf("changed %s", path)
					}
				}
			})
		}
	}
}

func TestPreflightCredentialTimeoutAndManagedPath(t *testing.T) {
	for _, hanging := range []bool{false, true} {
		t.Run(map[bool]string{false: "managed-path", true: "timeout"}[hanging], func(t *testing.T) {
			root := setupCLITemplate(t)
			bin := filepath.Join(root, ".platformctl", "bin")
			mkdir(t, bin)
			body := "exit 0"
			if hanging {
				body = "/bin/sleep 30"
			}
			checkStub(t, bin, "synthetic-credential", body)
			path := filepath.Join(root, "template", "platform.template.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, path, strings.Replace(string(data), "requirements:", "requirements:\n  credentials:\n    - name: synthetic\n      command: synthetic-credential", 1))
			start := time.Now()
			timeout := "2s"
			if hanging {
				timeout = "100ms"
			}
			p := workflowProcess(t, root, "preflight", "--strict", "--check-timeout", timeout, "--json")
			out, err := p.CombinedOutput()
			if (err != nil) != hanging || time.Since(start) > 3*time.Second {
				t.Fatalf("hanging=%v err=%v elapsed=%s %s", hanging, err, time.Since(start), out)
			}
			if hanging && !bytes.Contains(out, []byte("PLATFORMCTL_CREDENTIAL_FAILED")) {
				t.Fatalf("wrong error: %s", out)
			}
		})
	}
}

func TestPreflightNeverInstallsMissingManagedTool(t *testing.T) {
	root := setupCLITemplate(t)
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, path, strings.Replace(string(data), "- name: echo", "- name: terraform\n      version: 999.0.0", 1))
	p := workflowProcess(t, root, "preflight", "--strict", "--json")
	p.Env = append(p.Env, "PATH="+t.TempDir())
	out, err := p.CombinedOutput()
	if err == nil || !bytes.Contains(out, []byte("PLATFORMCTL_MISSING_TOOLS")) || exists(filepath.Join(root, ".platformctl")) {
		t.Fatalf("diagnostic installed or mutated: %v %s", err, out)
	}
}

func TestTargetAndBootstrapChangesInvalidateRecordedPlan(t *testing.T) {
	for _, change := range []struct{ old, new string }{
		{"111111111111", "222222222222"}, {"eu-central-1", "eu-west-1"}, {"profile-a", "profile-b"},
		{"context-a", "context-b"}, {"cluster-a", "cluster-b"}, {"https://server-a.invalid", "https://server-b.invalid"},
		{"bootstrap: false", "bootstrap: true"},
	} {
		for _, phase := range []string{"apply", "destroy"} {
			t.Run(phase+"/"+change.old, func(t *testing.T) {
				root := setupCLITemplate(t)
				configPath := filepath.Join(root, "platform.yaml")
				config, err := os.ReadFile(configPath)
				if err != nil {
					t.Fatal(err)
				}
				config = append(config, []byte("target:\n  bootstrap: false\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: profile-a\n  kubernetes:\n    context: context-a\n    cluster: cluster-a\n    server: https://server-a.invalid\n")...)
				writeCLIFile(t, configPath, string(config))
				bin := filepath.Join(root, "bin")
				mkdir(t, bin)
				for _, name := range []string{"aws", "kubectl"} {
					checkStub(t, bin, name, "touch identity-called; exit 1")
				}
				run := func(args ...string) ([]byte, error) {
					p := workflowProcess(t, root, args...)
					p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
					return p.CombinedOutput()
				}
				if out, err := run("plan", "--json"); err != nil {
					t.Fatalf("plan: %v %s", err, out)
				}
				before := approvalFiles(t, root)
				writeCLIFile(t, configPath, strings.Replace(string(config), change.old, change.new, 1))
				out, err := run(phase, "--yes", "--json")
				if err == nil || !bytes.Contains(out, []byte("PLATFORMCTL_PLAN_CHANGED")) || exists(filepath.Join(root, "identity-called")) {
					t.Fatalf("changed target reached execution: %v %s", err, out)
				}
				after := approvalFiles(t, root)
				if len(before) != len(after) {
					t.Fatal("state changed")
				}
				for path, value := range before {
					if after[path] != value {
						t.Fatalf("changed %s", path)
					}
				}
			})
		}
	}
}

func TestWorkflowUsesVerifiedAWSProfile(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(map[bool]string{false: "scoped-environment", true: "explicit-foreign-profile"}[override], func(t *testing.T) {
			root := setupCLITemplate(t)
			bin := filepath.Join(root, "bin")
			mkdir(t, bin)
			checkStub(t, bin, "aws", `case "$1" in
version|--version) echo 'aws-cli/2.0.0';;
sts) [ "$AWS_PROFILE" = profile-a ] && [ "$AWS_REGION" = eu-central-1 ] && [ -z "$AWS_ACCESS_KEY_ID" ] || exit 8; printf '{"Account":"111111111111"}';;
synthetic-mutate) printf '%s/%s/%s' "$AWS_PROFILE" "$AWS_REGION" "$AWS_ACCESS_KEY_ID" > executed-identity;;
*) exit 9;; esac`)
			path := filepath.Join(root, "template", "platform.template.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := strings.Replace(string(data), "- name: echo", "- name: aws", 1)
			manifest = strings.Replace(manifest, "command: echo", "command: aws", 1)
			args := `args: [synthetic-mutate]`
			if override {
				args = `args: [--profile, profile-b, synthetic-mutate]`
			}
			manifest = strings.Replace(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, args, 1)
			writeCLIFile(t, path, manifest)
			config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: profile-a\n")
			p := workflowProcess(t, root, "apply", "--yes", "--json")
			p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AWS_PROFILE=profile-c", "AWS_REGION=eu-west-1", "AWS_ACCESS_KEY_ID=synthetic-account-b", "AWS_SECRET_ACCESS_KEY=synthetic-account-b")
			out, err := p.CombinedOutput()
			if override {
				if err == nil || exists(filepath.Join(root, "executed-identity")) || exists(filepath.Join(root, "generated")) {
					t.Fatalf("foreign profile executed: %v %s", err, out)
				}
				return
			}
			identity, readErr := os.ReadFile(filepath.Join(root, "executed-identity"))
			if err != nil || readErr != nil || string(identity) != "profile-a/eu-central-1/" {
				t.Fatalf("executed identity=%q err=%v read=%v %s", identity, err, readErr, out)
			}
			// A completed workflow saved an actual approval. Changing the assertion
			// cannot reuse that approval with --resume, even when rendered files agree.
			if err := os.Remove(filepath.Join(root, "executed-identity")); err != nil {
				t.Fatal(err)
			}
			before := approvalFiles(t, root)
			config, err = os.ReadFile(filepath.Join(root, "platform.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, filepath.Join(root, "platform.yaml"), strings.Replace(string(config), "111111111111", "222222222222", 1))
			resumed := workflowProcess(t, root, "apply", "--resume", "--yes", "--json")
			resumed.Env = append([]string(nil), p.Env...)
			out, err = resumed.CombinedOutput()
			if err == nil || !bytes.Contains(out, []byte("PLATFORMCTL_RESUME_INVALID")) || exists(filepath.Join(root, "executed-identity")) {
				t.Fatalf("reused approval: %v %s", err, out)
			}
			after := approvalFiles(t, root)
			if len(before) != len(after) {
				t.Fatal("resume changed state")
			}
			for path, value := range before {
				if after[path] != value {
					t.Fatal("resume changed " + path)
				}
			}

		})
	}
}

func TestBootstrapChecksContextBeforeClusterStep(t *testing.T) {
	root := setupCLITemplate(t)
	bin := filepath.Join(root, "bin")
	mkdir(t, bin)
	checkStub(t, bin, "kubectl", `case "$1" in
version|--version) echo 'kubectl 1.35.0';;
config) [ -f ready ] || exit 1; printf '{"current-context":"expected","contexts":[{"name":"expected","context":{"cluster":"cluster"}}],"clusters":[{"name":"cluster","cluster":{"server":"https://cluster.invalid"}}]}';;
apply) touch mutated;;
*) exit 2;; esac`)
	writeCLIFile(t, filepath.Join(root, "template", "platform.template.yaml"), `apiVersion: platformctl.io/v1beta1
kind: PlatformTemplate
metadata: {name: bootstrap, description: Synthetic bootstrap}
inputs:
  project_name: {type: string, description: Synthetic name}
requirements:
  tools:
    - name: kubectl
files:
  - path: generated/result
    content: synthetic
steps:
  apply:
    - name: create synthetic cluster
      command: sh
      args: [-c, 'touch ready']
    - name: deploy synthetic cluster
      command: kubectl
      args: [apply]
  destroy:
    - name: synthetic destroy
      command: echo
      args: [destroy]
`)
	config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n  kubernetes:\n    context: expected\n    cluster: cluster\n    server: https://cluster.invalid\n")
	run := func(args ...string) ([]byte, error) {
		p := workflowProcess(t, root, args...)
		p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		return p.CombinedOutput()
	}
	if out, err := run("apply", "--yes", "--json"); err == nil || exists(filepath.Join(root, "ready")) {
		t.Fatalf("normal apply created missing cluster: %v %s", err, out)
	}
	config, err = os.ReadFile(filepath.Join(root, "platform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), strings.Replace(string(config), "target:\n", "target:\n  bootstrap: true\n", 1))
	if out, err := run("apply", "--yes", "--json"); err != nil || !exists(filepath.Join(root, "mutated")) {
		t.Fatalf("bootstrap: %v %s", err, out)
	}
}
