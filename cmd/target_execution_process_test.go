package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictAndInfrastructureRequireDeclaredTarget(t *testing.T) {
	for _, tool := range []string{"aws", "kubectl", "terraform"} {
		t.Run(tool, func(t *testing.T) {
			root := setupCLITemplate(t)
			path := filepath.Join(root, "template", "platform.template.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, path, strings.Replace(string(data), "command: echo", "command: "+tool, 1))
			bin := filepath.Join(root, "bin")
			mkdir(t, bin)
			checkStub(t, bin, tool, "touch identity-called; exit 0")
			for _, args := range [][]string{{"preflight", "--strict", "--json"}, {"apply", "--yes", "--json"}, {"destroy", "--yes", "--json"}} {
				p := workflowProcess(t, root, args...)
				p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				out, err := p.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "PLATFORMCTL_TARGET_REQUIRED") || exists(filepath.Join(root, "identity-called")) || exists(filepath.Join(root, "generated")) || exists(filepath.Join(root, ".platformctl", "state.json")) {
					t.Fatalf("missing required identity executed: %v %s", err, out)
				}
			}
			p := workflowProcess(t, root, "preflight", "--json")
			p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := p.CombinedOutput()
			if err != nil || !strings.Contains(string(out), `"ok": false`) {
				t.Fatalf("nonstrict diagnostics: %v %s", err, out)
			}
		})
	}
}

func TestRelativeKubeconfigUsesSameAbsoluteFileFromStepDirectory(t *testing.T) {
	root := setupCLITemplate(t)
	bin := filepath.Join(root, "bin")
	mkdir(t, bin)
	checkStub(t, bin, "kubectl", `case "$1" in version|--version) echo 'kubectl 1.35.0';; config) /bin/cat "$KUBECONFIG";; apply) /bin/cat "$KUBECONFIG" > actual-identity;; *) exit 2;; esac`)
	expected := `{"current-context":"expected","contexts":[{"name":"expected","context":{"cluster":"expected"}}],"clusters":[{"name":"expected","cluster":{"server":"https://expected.invalid"}}]}`
	writeCLIFile(t, filepath.Join(root, "selected.config"), expected)
	writeCLIFile(t, filepath.Join(root, "generated", "selected.config"), "FOREIGN CLUSTER")
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(string(data), "- name: echo", "- name: kubectl", 1)
	manifest = strings.Replace(manifest, "command: echo", "command: kubectl\n      dir: generated", 1)
	manifest = strings.Replace(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, `args: [apply]`, 1)
	writeCLIFile(t, path, manifest)
	config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n  kubernetes:\n    context: expected\n    cluster: expected\n    server: https://expected.invalid\n")
	p := workflowProcess(t, root, "apply", "--yes", "--json")
	p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "KUBECONFIG=selected.config")
	out, err := p.CombinedOutput()
	actual, readErr := os.ReadFile(filepath.Join(root, "generated", "actual-identity"))
	if err != nil || readErr != nil || string(actual) != expected {
		t.Fatalf("check/execution differed: actual=%q err=%v read=%v %s", actual, err, readErr, out)
	}
}

func TestRelativeAWSConfigsUseSameAbsoluteFilesFromStepDirectory(t *testing.T) {
	root := setupCLITemplate(t)
	bin := filepath.Join(root, "bin")
	mkdir(t, bin)
	checkStub(t, bin, "aws", `case "$1" in
--version) echo 'aws-cli/2.0.0';;
sts) if [ "$(/bin/cat "$AWS_CONFIG_FILE")" = config-a ] && [ "$(/bin/cat "$AWS_SHARED_CREDENTIALS_FILE")" = credentials-a ]; then printf '{"Account":"111111111111"}'; else printf '{"Account":"222222222222"}'; fi;;
synthetic-mutate) /bin/cat "$AWS_CONFIG_FILE" "$AWS_SHARED_CREDENTIALS_FILE" > actual-identity;;
*) exit 2;; esac`)
	for path, value := range map[string]string{"selected.config": "config-a", "selected.credentials": "credentials-a", "generated/selected.config": "config-b", "generated/selected.credentials": "credentials-b"} {
		writeCLIFile(t, filepath.Join(root, path), value)
	}
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(string(data), "- name: echo", "- name: aws", 1)
	manifest = strings.Replace(manifest, "command: echo", "command: aws\n      dir: generated", 1)
	manifest = strings.Replace(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, `args: [synthetic-mutate]`, 1)
	writeCLIFile(t, path, manifest)
	config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n")
	p := workflowProcess(t, root, "apply", "--yes", "--json")
	p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AWS_CONFIG_FILE=selected.config", "AWS_SHARED_CREDENTIALS_FILE=selected.credentials")
	out, err := p.CombinedOutput()
	actual, readErr := os.ReadFile(filepath.Join(root, "generated", "actual-identity"))
	if err != nil || readErr != nil || string(actual) != "config-acredentials-a" {
		t.Fatalf("AWS config identity differed: actual=%q err=%v read=%v %s", actual, err, readErr, out)
	}
}

func TestCredentialProviderUsesActualStepDirectory(t *testing.T) {
	root := setupCLITemplate(t)
	bin := filepath.Join(root, "bin")
	mkdir(t, bin)
	checkStub(t, root, "credential-provider", "printf '111111111111'")
	mkdir(t, filepath.Join(root, "generated"))
	checkStub(t, filepath.Join(root, "generated"), "credential-provider", "printf '222222222222'")
	checkStub(t, bin, "aws", `case "$1" in --version) echo 'aws-cli/2.0.0';; sts) printf '{"Account":"%s"}' "$(./credential-provider)";; *) touch provider-mutated;; esac`)
	path := filepath.Join(root, "template", "platform.template.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	manifest := strings.Replace(string(data), "- name: echo", "- name: aws", 1)
	manifest = strings.Replace(manifest, "command: echo", "command: aws\n      dir: generated", 1)
	manifest = strings.Replace(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, `args: [synthetic-mutate]`, 1)
	writeCLIFile(t, path, manifest)
	config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n")
	p := workflowProcess(t, root, "apply", "--yes", "--json")
	p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := p.CombinedOutput()
	if err == nil || exists(filepath.Join(root, "generated", "provider-mutated")) || exists(filepath.Join(root, "generated", "out.txt")) || exists(filepath.Join(root, ".platformctl", "state.json")) || !strings.Contains(string(out), "AWS account") {
		t.Fatalf("credential provider cwd bypass: %v %s", err, out)
	}
}

func TestDirectEKSForeignNameRefusedBeforeMutation(t *testing.T) {
	for _, operation := range []string{"create-cluster", "update-kubeconfig"} {
		t.Run(operation, func(t *testing.T) {
			root := setupCLITemplate(t)
			bin := filepath.Join(root, "bin")
			mkdir(t, bin)
			checkStub(t, bin, "aws", `case "$1" in --version) echo 'aws-cli/2.0.0';; sts) printf '{"Account":"111111111111"}';; eks) touch unexpected-mutation;; *) exit 2;; esac`)
			path := filepath.Join(root, "template", "platform.template.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := strings.Replace(string(data), "inputs:\n", "inputs:\n  cluster_name: {type: string, description: Expected cluster, default: cluster-a}\n", 1)
			manifest = strings.Replace(manifest, "- name: echo", "- name: aws", 1)
			manifest = strings.Replace(manifest, "command: echo", "command: aws", 1)
			manifest = strings.Replace(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, `args: [eks, `+operation+`, --name, cluster-b]`, 1)
			writeCLIFile(t, path, manifest)
			config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n  cluster_name: cluster-a\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n  kubernetes:\n    kubeconfig: selected.config\n    context: expected\n    cluster: expected\n    server: https://expected.invalid\n")
			p := workflowProcess(t, root, "apply", "--yes", "--json")
			p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			out, err := p.CombinedOutput()
			if err == nil || !strings.Contains(string(out), "PLATFORMCTL_TARGET_MISMATCH") || exists(filepath.Join(root, "unexpected-mutation")) || exists(filepath.Join(root, "generated")) || exists(filepath.Join(root, ".platformctl", "state.json")) {
				t.Fatalf("foreign EKS name executed: %v %s", err, out)
			}
		})
	}
}

func TestDirectClusterAndConfigWritesRequireCompleteTarget(t *testing.T) {
	for _, tc := range []struct {
		name, command, args, target string
	}{
		{"eks-missing-cluster", "aws", "[eks, delete-cluster, --name, arbitrary-foreign-cluster]", "  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n"},
		{"kind-missing-cluster", "kind", "[delete, cluster, --name, arbitrary-foreign-cluster]", "  kubernetes:\n    context: expected\n    cluster: expected\n    server: https://expected.invalid\n"},
		{"eks-config-no-kubernetes", "aws", "[eks, update-kubeconfig, --name, cluster-a]", "  cluster_name: cluster-a\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n"},
		{"eks-config-no-scoped-file", "aws", "[eks, --region, eu-central-1, update-kubeconfig, --name, cluster-a]", "  cluster_name: cluster-a\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n  kubernetes:\n    context: expected\n    cluster: expected\n    server: https://expected.invalid\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := setupCLITemplate(t)
			bin := filepath.Join(root, "bin")
			mkdir(t, bin)
			// Any diagnostic or mutating invocation would leave a sentinel.
			// The fixture never reads or writes an operator's kubeconfig.
			checkStub(t, bin, tc.command, "touch unexpected-tool-call; exit 0")
			path := filepath.Join(root, "template", "platform.template.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := strings.Replace(string(data), "inputs:\n", "inputs:\n  cluster_name: {type: string, description: Expected cluster, default: cluster-a}\n", 1)
			manifest = strings.Replace(manifest, "- name: echo", "- name: "+tc.command, 1)
			manifest = strings.ReplaceAll(manifest, "command: echo", "command: "+tc.command)
			manifest = strings.ReplaceAll(manifest, `args: ["apply", "{{ .Values.project_name }}"]`, "args: "+tc.args)
			manifest = strings.ReplaceAll(manifest, `args: ["destroy", "{{ .Values.project_name }}"]`, "args: "+tc.args)
			writeCLIFile(t, path, manifest)
			config, err := os.ReadFile(filepath.Join(root, "platform.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			writeCLIFile(t, filepath.Join(root, "platform.yaml"), string(config)+"target:\n"+tc.target)
			for _, args := range [][]string{{"preflight", "--strict", "--json"}, {"apply", "--yes", "--json"}, {"destroy", "--yes", "--json"}} {
				p := workflowProcess(t, root, args...)
				p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				out, err := p.CombinedOutput()
				if err == nil || !strings.Contains(string(out), "PLATFORMCTL_TARGET_REQUIRED") || exists(filepath.Join(root, "unexpected-tool-call")) || exists(filepath.Join(root, "generated")) || exists(filepath.Join(root, ".platformctl", "state.json")) {
					t.Fatalf("incomplete target executed: args=%v err=%v output=%s", args, err, out)
				}
			}
		})
	}
}
