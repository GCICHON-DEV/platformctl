package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"platformctl/internal/state"
)

const bootstrapCA = "c3ludGhldGljLWNh"
const kubeSecretCanary = "synthetic-kube-secret-canary"

func bootstrapFixture(t *testing.T, eks bool) (string, func(...string) ([]byte, error)) {
	t.Helper()
	root := setupCLITemplate(t)
	bin := filepath.Join(root, "bin")
	mkdir(t, bin)
	contextName, clusterName, server := "kind-demo", "kind-demo", "https://127.0.0.1:24443"
	if eks {
		contextName = "arn:aws:eks:eu-central-1:111111111111:cluster/demo"
		clusterName = contextName
		server = "https://demo.eks.invalid"
	}
	config := map[string]interface{}{"apiVersion": "v1", "kind": "Config", "current-context": contextName, "contexts": []interface{}{map[string]interface{}{"name": contextName, "context": map[string]interface{}{"cluster": clusterName, "user": "synthetic"}}}, "clusters": []interface{}{map[string]interface{}{"name": clusterName, "cluster": map[string]interface{}{"server": server, "certificate-authority-data": bootstrapCA}}}, "users": []interface{}{map[string]interface{}{"name": "synthetic", "user": map[string]interface{}{"token": kubeSecretCanary}}}}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	writeScript := `umask 077
printf '%s' "$SYNTHETIC_KUBE_BYTES" > "$KUBECONFIG"
case "$SYNTHETIC_BOOTSTRAP_FAILURE" in
crash) kill -KILL "$PPID"; exit 0;;
other-config) /bin/mkdir -p "$HOME/.aws"; printf foreign > "$HOME/.aws/config";;
generated) printf changed > generated/out.txt;;
save-failure) /bin/mv "$HOME/.platformctl/state.json" "$HOME/.platformctl/pending-checkpoint.json"; /bin/mkdir "$HOME/.platformctl/state.json";;
foreign-context) printf '{"current-context":"foreign"}' > "$KUBECONFIG";;
esac`
	checkStub(t, bin, "kind", `case "$1" in --version) echo 'kind v0.30.0';; create) `+writeScript+`;; *) exit 2;; esac`)
	checkStub(t, bin, "aws", `case "$1" in
--version) echo 'aws-cli/2.0.0';;
sts) printf '{"Account":"111111111111"}';;
eks) case "$2" in
update-kubeconfig) `+writeScript+`;;
describe-cluster) printf '%s' "$SYNTHETIC_EKS_BYTES";;
*) exit 2;; esac;;
*) exit 2;; esac`)
	checkStub(t, bin, "kubectl", `case "$1" in
version|--version) echo 'kubectl 1.35.0';;
config) /bin/cat "$KUBECONFIG";;
apply) printf '%s' "$KUBECONFIG" > executed-config; touch cluster-mutated;;
*) exit 2;; esac`)
	tools, command, args := "    - name: kind\n    - name: kubectl\n", "kind", `[create, cluster, --name, demo]`
	target := "target:\n  bootstrap: true\n  cluster_name: demo\n  kubernetes:\n    endpoint_source: kind\n    kubeconfig: .platformctl/targets/demo.kubeconfig\n    context: kind-demo\n    cluster: kind-demo\n    server: https://127.0.0.1:24443\n"
	if eks {
		tools = "    - name: aws\n    - name: kubectl\n"
		command = "aws"
		args = `[eks, update-kubeconfig, --name, demo, --profile, synthetic, --region, eu-central-1]`
		target = "target:\n  bootstrap: true\n  cluster_name: demo\n  aws:\n    account: '111111111111'\n    region: eu-central-1\n    profile: synthetic\n  kubernetes:\n    endpoint_source: eks\n    kubeconfig: .platformctl/targets/demo.kubeconfig\n    context: " + contextName + "\n    cluster: " + clusterName + "\n"
	}
	writeCLIFile(t, filepath.Join(root, "template", "platform.template.yaml"), `apiVersion: platformctl.io/v1beta1
kind: PlatformTemplate
metadata: {name: bootstrap, description: Synthetic bootstrap}
inputs:
  project_name: {type: string, description: Synthetic project}
  cluster_name: {type: string, description: Synthetic cluster, default: demo}
requirements:
  tools:
`+tools+`files:
  - path: generated/out.txt
    content: synthetic
steps:
  apply:
    - name: create synthetic cluster target
      command: `+command+`
      args: `+args+`
      establishes_cluster_target: true
    - name: deploy synthetic cluster
      command: kubectl
      args: [apply]
  destroy:
    - name: synthetic destroy
      command: echo
      args: [destroy]
`)
	writeCLIFile(t, filepath.Join(root, "platform.yaml"), "template:\n  source: ./template\nvalues:\n  project_name: demo\n"+target)
	eksDescription := map[string]interface{}{"cluster": map[string]interface{}{"name": "demo", "arn": "arn:aws:eks:eu-central-1:111111111111:cluster/demo", "status": "ACTIVE", "endpoint": "https://demo.eks.invalid", "certificateAuthority": map[string]string{"data": bootstrapCA}}}
	eksBytes, err := json.Marshal(eksDescription)
	if err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) ([]byte, error) {
		response := string(eksBytes)
		if failure := os.Getenv("SYNTHETIC_EKS_FAILURE"); failure != "" {
			var modified map[string]interface{}
			if err := json.Unmarshal(eksBytes, &modified); err != nil {
				t.Fatal(err)
			}
			cluster := modified["cluster"].(map[string]interface{})
			switch failure {
			case "account":
				cluster["arn"] = "arn:aws:eks:eu-central-1:222222222222:cluster/demo"
			case "region":
				cluster["arn"] = "arn:aws:eks:eu-west-1:111111111111:cluster/demo"
			case "endpoint":
				cluster["endpoint"] = "https://foreign.invalid"
			case "ca":
				cluster["certificateAuthority"] = map[string]string{"data": "b3RoZXItY2E="}
			case "name":
				cluster["name"] = "foreign"
			case "status":
				cluster["status"] = "CREATING"
			}
			data, err := json.Marshal(modified)
			if err != nil {
				t.Fatal(err)
			}
			response = string(data)
		}
		p := workflowProcess(t, root, args...)
		p.Env = append(p.Env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "AWS_CONFIG_FILE="+filepath.Join(root, ".aws", "config"), "AWS_SHARED_CREDENTIALS_FILE="+filepath.Join(root, ".aws", "credentials"), "SYNTHETIC_KUBE_BYTES="+string(encoded), "SYNTHETIC_EKS_BYTES="+response)
		out, err := p.CombinedOutput()
		if strings.Contains(string(out), kubeSecretCanary) {
			t.Fatalf("private kubeconfig leaked: %s", out)
		}
		return out, err
	}
	return root, run
}

func readBootstrapState(t *testing.T, root string, name ...string) *state.State {
	t.Helper()
	file := "state.json"
	if len(name) > 0 {
		file = name[0]
	}
	data, err := os.ReadFile(filepath.Join(root, ".platformctl", file))
	if err != nil {
		t.Fatal(err)
	}
	var st state.State
	if err = json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return &st
}

func TestBootstrapRealConfigTransitionAndResume(t *testing.T) {
	for _, eks := range []bool{false, true} {
		t.Run(map[bool]string{false: "kind", true: "eks"}[eks], func(t *testing.T) {
			root, run := bootstrapFixture(t, eks)
			if out, err := run("plan", "--json"); err != nil {
				t.Fatalf("plan: %v %s", err, out)
			}
			origin := readBootstrapState(t, root).LastPlanHash
			if out, err := run("apply", "--yes", "--json"); err != nil {
				t.Fatalf("apply: %v %s", err, out)
			}
			if !exists(filepath.Join(root, "cluster-mutated")) {
				t.Fatal("cluster step did not execute")
			}
			st := readBootstrapState(t, root)
			var receipt bootstrapReceipt
			if json.Unmarshal([]byte(st.Metadata[bootstrapReceiptKey]), &receipt) != nil || receipt.OriginApprovedHash != origin || receipt.PolicyAcceptedHash == origin || receipt.PolicyAcceptedHash != st.ApprovedPlanHash || receipt.ObservedHash == "" || !st.CompletedSteps[receipt.Marker] || st.Metadata[bootstrapPendingKey] != "" {
				t.Fatalf("invalid atomic receipt: %+v state=%+v", receipt, st)
			}
			canonicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			executed, err := os.ReadFile(filepath.Join(root, "executed-config"))
			if err != nil || string(executed) != filepath.Join(canonicalRoot, ".platformctl", "targets", "demo.kubeconfig") {
				t.Fatalf("actual config=%q %v", executed, err)
			}
			if out, err := run("apply", "--resume", "--yes", "--json"); err != nil {
				t.Fatalf("valid receipt resume: %v %s", err, out)
			}
			path := filepath.Join(root, ".platformctl", "targets", "demo.kubeconfig")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(path, append(data, ' '), 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := run("apply", "--resume", "--yes", "--json"); err == nil {
				t.Fatalf("changed receipt bytes resumed: %s", out)
			}
		})
	}
}

func TestBootstrapFailureHasNoReceiptOrClusterMutation(t *testing.T) {
	for _, failure := range []string{"crash", "other-config", "generated", "foreign-context", "save-failure"} {
		t.Run(failure, func(t *testing.T) {
			root, run := bootstrapFixture(t, false)
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(root, ".platformctl"), 0700) })
			t.Setenv("SYNTHETIC_BOOTSTRAP_FAILURE", failure)
			if out, err := run("apply", "--yes", "--json"); err == nil {
				t.Fatalf("accepted %s: %s", failure, out)
			}
			var st *state.State
			if failure == "save-failure" {
				st = readBootstrapState(t, root, "pending-checkpoint.json")
			} else {
				st = readBootstrapState(t, root)
			}
			if exists(filepath.Join(root, "cluster-mutated")) || st.Metadata[bootstrapReceiptKey] != "" || st.Metadata[bootstrapPendingKey] == "" {
				t.Fatalf("unsafe transition: %+v", st)
			}
			if out, err := run("apply", "--resume", "--yes", "--json"); err == nil {
				t.Fatalf("recovered bootstrap without receipt: %s", out)
			}
		})
	}
}

func TestBootstrapRefusesExistingOrForeignFile(t *testing.T) {
	for _, kind := range []string{"existing", "symlink", "hardlink", "parent-symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, run := bootstrapFixture(t, false)
			directory := filepath.Join(root, ".platformctl", "targets")
			mkdir(t, filepath.Join(root, ".platformctl"))
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("foreign-fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if kind == "parent-symlink" {
				if err := os.Symlink(filepath.Dir(outside), directory); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(directory, "demo.kubeconfig")
				switch kind {
				case "existing":
					if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(outside, path); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(outside, path); err != nil {
						t.Fatal(err)
					}
				}
			}
			if out, err := run("apply", "--yes", "--json"); err == nil || exists(filepath.Join(root, "cluster-mutated")) {
				t.Fatalf("adopted %s: %v %s", kind, err, out)
			}
			data, err := os.ReadFile(outside)
			if err != nil || string(data) != "foreign-fixture" {
				t.Fatal("outside fixture changed")
			}
		})
	}
}

func TestEKSMetadataCannotApproveForeignCluster(t *testing.T) {
	for _, failure := range []string{"account", "region", "endpoint", "ca", "name", "status"} {
		t.Run(failure, func(t *testing.T) {
			root, run := bootstrapFixture(t, true)
			t.Setenv("SYNTHETIC_EKS_FAILURE", failure)
			if out, err := run("apply", "--yes", "--json"); err == nil {
				t.Fatalf("foreign EKS metadata accepted: %s", out)
			}
			st := readBootstrapState(t, root)
			if exists(filepath.Join(root, "cluster-mutated")) || st.Metadata[bootstrapReceiptKey] != "" || st.Metadata[bootstrapPendingKey] == "" {
				t.Fatalf("EKS bypass: %+v", st)
			}
		})
	}
}

func TestBootstrapMarkerContractAndOriginReceipt(t *testing.T) {
	for _, problem := range []string{"missing", "multiple", "retry", "before-marker"} {
		t.Run(problem, func(t *testing.T) {
			root, run := bootstrapFixture(t, false)
			path := filepath.Join(root, "template", "platform.template.yaml")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifest := string(data)
			switch problem {
			case "missing":
				manifest = strings.Replace(manifest, "      establishes_cluster_target: true\n", "", 1)
			case "multiple":
				manifest = strings.Replace(manifest, "    - name: deploy synthetic cluster", "    - name: second marker\n      command: echo\n      establishes_cluster_target: true\n    - name: deploy synthetic cluster", 1)
			case "retry":
				manifest = strings.Replace(manifest, "      establishes_cluster_target: true", "      retry: {attempts: 2}\n      establishes_cluster_target: true", 1)
			case "before-marker":
				manifest = strings.Replace(manifest, "  apply:\n", "  apply:\n    - name: premature cluster mutation\n      command: kubectl\n      args: [apply]\n", 1)
			}
			writeCLIFile(t, path, manifest)
			if out, err := run("apply", "--yes", "--json"); err == nil || exists(filepath.Join(root, "cluster-mutated")) || exists(filepath.Join(root, "generated")) {
				t.Fatalf("invalid bootstrap marker executed: %v %s", err, out)
			}
		})
	}
	t.Run("unrelated-origin", func(t *testing.T) {
		root, run := bootstrapFixture(t, false)
		if out, err := run("apply", "--yes", "--json"); err != nil {
			t.Fatalf("initial apply: %v %s", err, out)
		}
		st := readBootstrapState(t, root)
		var receipt bootstrapReceipt
		if err := json.Unmarshal([]byte(st.Metadata[bootstrapReceiptKey]), &receipt); err != nil {
			t.Fatal(err)
		}
		receipt.OriginApprovedHash = strings.Repeat("a", 64)
		encoded, err := json.Marshal(receipt)
		if err != nil {
			t.Fatal(err)
		}
		st.Metadata[bootstrapReceiptKey] = string(encoded)
		encoded, err = json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, ".platformctl", "state.json"), encoded, 0600); err != nil {
			t.Fatal(err)
		}
		if out, err := run("apply", "--resume", "--yes", "--json"); err == nil || !strings.Contains(string(out), "original approval") {
			t.Fatalf("unrelated origin accepted: %v %s", err, out)
		}
	})
}
