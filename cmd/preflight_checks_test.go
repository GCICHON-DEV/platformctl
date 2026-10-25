package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"platformctl/internal/templateengine"
)

func checkStub(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestPreflightPhaseAndDeadlines(t *testing.T) {
	root := setupCLITemplate(t)
	chdirForTest(t, root)
	bin := t.TempDir()
	checkStub(t, bin, "docker", "/bin/sleep 30")
	checkStub(t, bin, "kubectl", "exit 7")
	resolved := &templateengine.Resolved{Manifest: templateengine.Manifest{Steps: templateengine.Steps{Apply: []templateengine.Step{{Command: "kubectl"}}, Destroy: []templateengine.Step{{Command: "kubectl"}}}, Requirements: templateengine.Requirements{Tools: []templateengine.Tool{{Name: "docker"}, {Name: "kubectl"}}}}}
	for _, phase := range []string{"bootstrap", "apply", "destroy"} {
		start := time.Now()
		checks := localReadinessChecks(context.Background(), 100*time.Millisecond, bin, resolved, phase)
		if time.Since(start) > time.Second {
			t.Fatal("unbounded readiness check")
		}
		foundContext := false
		for _, check := range checks {
			if check.Name == "docker daemon" && check.OK {
				t.Fatal("hanging daemon accepted")
			}
			if check.Name == "kubectl current context" {
				foundContext = true
				if check.OK {
					t.Fatal("bad context accepted")
				}
			}
		}
		if foundContext != (phase != "bootstrap") {
			t.Fatalf("phase=%s context=%v", phase, foundContext)
		}
	}
}

func TestTargetIdentityMismatch(t *testing.T) {
	bin := t.TempDir()
	checkStub(t, bin, "aws", `case "$1" in
sts) printf '{"Account":"111111111111"}';;
configure) printf 'eu-central-1';;
*) exit 2;; esac`)
	checkStub(t, bin, "kubectl", `printf '{"current-context":"expected","contexts":[{"name":"expected","context":{"cluster":"cluster"}}],"clusters":[{"name":"cluster","cluster":{"server":"https://cluster.invalid"}}]}'`)
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	resolved := &templateengine.Resolved{Values: map[string]interface{}{"region": "eu-central-1", "aws_profile": "synthetic"}, Platform: templateengine.PlatformFile{Target: &templateengine.Target{AWS: &templateengine.AWSTarget{Account: "111111111111", Region: "eu-central-1", Profile: "synthetic"}, Kubernetes: &templateengine.KubernetesTarget{Context: "expected", Cluster: "cluster", Server: "https://cluster.invalid"}}}}
	if err := requireTarget(context.Background(), bin, resolved, true); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"account", "region", "context", "cluster", "server"} {
		t.Run(name, func(t *testing.T) {
			clone := *resolved.Platform.Target
			aws := *clone.AWS
			kube := *clone.Kubernetes
			clone.AWS = &aws
			clone.Kubernetes = &kube
			copyResolved := *resolved
			copyResolved.Platform.Target = &clone
			switch name {
			case "account":
				aws.Account = "222222222222"
			case "region":
				aws.Region = "eu-west-1"
			case "context":
				kube.Context = "foreign"
			case "cluster":
				kube.Cluster = "foreign"
			case "server":
				kube.Server = "https://foreign.invalid"
			}
			if err := requireTarget(context.Background(), bin, &copyResolved, true); err == nil {
				t.Fatal("mismatch accepted")
			}
		})
	}
	resolved.Platform.Target.Kubernetes.Context = "foreign"
	if err := requireTarget(context.Background(), bin, resolved, false); err != nil {
		t.Fatalf("bootstrap demanded existing cluster: %v", err)
	}
}

func TestWorkspaceStateReadiness(t *testing.T) {
	root := t.TempDir()
	chdirForTest(t, root)
	if check := workspaceCheck("state", ".platformctl"); !check.OK {
		t.Fatal(check)
	}
	if _, err := os.Stat(".platformctl"); !os.IsNotExist(err) {
		t.Fatal("probe created state directory")
	}
	if err := os.Symlink(t.TempDir(), ".platformctl"); err != nil {
		t.Fatal(err)
	}
	if check := workspaceCheck("state", ".platformctl"); check.OK {
		t.Fatal("symlink state accepted")
	}
	if err := os.Remove(".platformctl"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0700) })
	if check := workspaceCheck("workspace", "."); check.OK {
		t.Fatal("readonly workspace accepted")
	}
}

func TestClusterStepContract(t *testing.T) {
	for _, name := range []string{"kubectl", "helm"} {
		if !clusterStep(templateengine.PlannedStep{Command: name, Args: []string{"apply"}}) {
			t.Fatal(name)
		}
	}
	if clusterStep(templateengine.PlannedStep{Command: "helm", Args: []string{"repo", "update"}}) {
		t.Fatal("repo command needs no cluster")
	}
	if !clusterStep(templateengine.PlannedStep{Command: "sh", RequiresCluster: true}) {
		t.Fatal("custom cluster guard lost")
	}
}

func TestConflictingDirectTargetSelectors(t *testing.T) {
	r := &templateengine.Resolved{Values: map[string]interface{}{"region": "eu-central-1", "aws_profile": "a"}, Platform: templateengine.PlatformFile{Target: &templateengine.Target{AWS: &templateengine.AWSTarget{Account: "111111111111", Region: "eu-central-1", Profile: "a"}, Kubernetes: &templateengine.KubernetesTarget{Context: "a", Cluster: "cluster-a", Server: "https://a.invalid"}}}}
	r.Platform.Target.ClusterName = "cluster-a"
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"aws", []string{"--profile", "b", "sts", "get-caller-identity"}},
		{"aws", []string{"eks", "create-cluster", "--name", "cluster-b"}},
		{"aws", []string{"eks", "update-kubeconfig", "--name", "cluster-b"}},
		{"aws", []string{"eks", "update-kubeconfig", "--name", "cluster-a", "--kubeconfig", "/foreign"}},
		{"kind", []string{"create", "cluster", "--name", "cluster-a", "--kubeconfig", "/foreign"}},
		{"aws", []string{"--profile=a", "--profile=b", "sts", "get-caller-identity"}},
		{"aws", []string{"--prof", "b", "sts", "get-caller-identity"}},
		{"aws", []string{"--region=eu-west-1", "eks", "list-clusters"}},
		{"aws", []string{"--endpoint-url=https://b.invalid", "sts", "get-caller-identity"}},
		{"kubectl", []string{"--context=b", "get", "nodes"}},
		{"kubectl", []string{"-shttps://b.invalid", "get", "nodes"}},
		{"kubectl", []string{"--kubeconfig", "other", "get", "nodes"}},
		{"helm", []string{"--kube-context=b", "upgrade", "release", "chart"}},
		{"terraform", []string{"-chdir=other", "apply"}},
	} {
		if err := validateStepTarget(r, templateengine.PlannedStep{Command: tc.command, Args: tc.args}); err == nil {
			t.Fatalf("accepted %s %v", tc.command, tc.args)
		}
	}
	for _, tc := range []struct {
		command string
		args    []string
	}{
		{"aws", []string{"--profile=a", "--region=eu-central-1", "sts", "get-caller-identity"}},
		{"kubectl", []string{"--context=a", "--cluster=cluster-a", "-shttps://a.invalid", "get", "nodes"}},
		{"terraform", []string{"apply"}},
	} {
		if err := validateStepTarget(r, templateengine.PlannedStep{Command: tc.command, Args: tc.args}); err != nil {
			t.Fatalf("legal %s %v: %v", tc.command, tc.args, err)
		}
	}
	delete(r.Values, "aws_profile")
	if err := validateStepTarget(r, templateengine.PlannedStep{Command: "terraform", Args: []string{"apply"}}); err == nil {
		t.Fatal("unverified provider input accepted")
	}
	r.Platform.Target = nil
	if err := validateStepTarget(r, templateengine.PlannedStep{Command: "aws", Args: []string{"--profile=b"}}); err != nil {
		t.Fatal("legacy compatibility changed")
	}
}
