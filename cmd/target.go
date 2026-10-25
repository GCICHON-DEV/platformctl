package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"platformctl/internal/apperror"
	"platformctl/internal/checkexec"
	"platformctl/internal/templateengine"
)

var accountID = regexp.MustCompile(`^[0-9]{12}$`)
var regionID = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)
var clusterNameID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,99}$`)

func requiredTargetChecks(resolved *templateengine.Resolved) []checkStatus {
	aws, kube, namedCluster, scopedKubeconfig := false, false, false, false
	inspect := func(name string) {
		switch filepath.Base(name) {
		case "aws", "terraform":
			aws = true
		case "kubectl", "helm", "kind":
			kube = true
		}
	}
	for _, tool := range resolved.RequirementTools() {
		inspect(tool.Name)
	}
	for _, phase := range []string{"apply", "destroy"} {
		for _, step := range resolved.StepsFor(phase) {
			command, err := resolved.Render(step.Command)
			if err == nil {
				inspect(command)
			} else {
				return []checkStatus{{Name: "workflow identity", Message: "workflow command could not be resolved"}}
			}
			if step.RequiresCluster || step.EstablishesClusterTarget {
				kube = true
			}
			args := make([]string, len(step.Args))
			for i, arg := range step.Args {
				args[i], err = resolved.Render(arg)
				if err != nil {
					return []checkStatus{{Name: "workflow identity", Message: "workflow arguments could not be resolved"}}
				}
			}
			eks, configWrite := directEKSStep(command, args)
			namedCluster = namedCluster || eks || filepath.Base(command) == "kind"
			scopedKubeconfig = scopedKubeconfig || configWrite
		}
	}
	var checks []checkStatus
	target := resolved.Platform.Target
	if aws && (target == nil || target.AWS == nil) {
		checks = append(checks, checkStatus{Name: "required AWS identity", Message: "infrastructure workflow requires target.aws account/region/profile; configure target and review a new plan"})
	}
	if kube && (target == nil || target.Kubernetes == nil) {
		checks = append(checks, checkStatus{Name: "required Kubernetes identity", Message: "infrastructure workflow requires target.kubernetes context/cluster/server; configure target and review a new plan"})
	}
	if namedCluster && (target == nil || !clusterNameID.MatchString(target.ClusterName)) {
		checks = append(checks, checkStatus{Name: "required cluster name", Message: "direct EKS/Kind workflow requires target.cluster_name; configure the expected cluster and review a new plan"})
	}
	if scopedKubeconfig && (target == nil || target.Kubernetes == nil || target.Kubernetes.Kubeconfig == "") {
		checks = append(checks, checkStatus{Name: "required scoped kubeconfig", Message: "EKS update-kubeconfig requires target.kubernetes and an explicit kubeconfig path; default/global config writes are refused"})
	}
	return checks
}

// Recognize the direct service even when AWS global options precede it. This
// deliberately fails closed for an ambiguous direct argument vector; arbitrary
// shell/provider programs remain the documented trusted-template boundary.
func directEKSStep(command string, args []string) (bool, bool) {
	if filepath.Base(command) != "aws" {
		return false, false
	}
	for i, arg := range args {
		if arg == "eks" {
			for _, remaining := range args[i+1:] {
				if remaining == "update-kubeconfig" {
					return true, true
				}
			}
			return true, false
		}
	}
	return false, false
}

func requireDeclaredTarget(resolved *templateengine.Resolved) error {
	if checks := requiredTargetChecks(resolved); len(checks) > 0 {
		return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_TARGET_REQUIRED", checks[0].Message)
	}
	return nil
}

func targetChecks(ctx context.Context, timeout time.Duration, pathEnv string, resolved *templateengine.Resolved, cluster bool) []checkStatus {
	return targetChecksAt(ctx, timeout, pathEnv, resolved, cluster, "")
}

func targetChecksAt(ctx context.Context, timeout time.Duration, pathEnv string, resolved *templateengine.Resolved, cluster bool, directory string) []checkStatus {
	target := resolved.Platform.Target
	if target == nil {
		return nil
	}
	var checks []checkStatus
	accountVerified := false
	fail := func(name, message string) { checks = append(checks, checkStatus{Name: name, Message: message}) }
	if target.ClusterName != "" {
		if value, ok := resolved.Values["cluster_name"]; !clusterNameID.MatchString(target.ClusterName) || !ok || fmt.Sprint(value) != target.ClusterName {
			fail("target cluster name", "target.cluster_name must match values.cluster_name")
		} else {
			checks = append(checks, checkStatus{Name: "target cluster name", OK: true})
		}
	}
	if aws := target.AWS; aws != nil {
		valid := accountID.MatchString(aws.Account) && regionID.MatchString(aws.Region) && validSelector(aws.Profile)
		for _, item := range os.Environ() {
			key, value, _ := strings.Cut(item, "=")
			if strings.HasPrefix(key, "AWS_ENDPOINT_URL") && value != "" {
				valid = false
			}
		}
		for key, expected := range map[string]string{"region": aws.Region, "aws_profile": aws.Profile} {
			if value, ok := resolved.Values[key]; ok && fmt.Sprint(value) != expected {
				valid = false
			}
		}
		if !valid {
			fail("AWS target", "target.aws requires a 12-digit account, region and profile matching declared input selectors")
		} else {
			args := []string{"sts", "get-caller-identity", "--profile", aws.Profile, "--region", aws.Region, "--output", "json"}
			out, err := targetOutput(ctx, timeout, pathEnv, target, directory, "aws", args...)
			var identity struct {
				Account string `json:"Account"`
			}
			if err != nil || json.Unmarshal(out, &identity) != nil || identity.Account != aws.Account {
				fail("AWS account", "AWS identity check failed or returned a different account")
			} else {
				checks = append(checks, checkStatus{Name: "AWS account", OK: true})
				accountVerified = true
			}
			checks = append(checks, checkStatus{Name: "AWS region", OK: true, Message: "workflow region is scoped to target.aws.region"})
		}
	}
	if kube := target.Kubernetes; kube != nil {
		for key, expected := range map[string]string{"HELM_KUBECONTEXT": kube.Context, "HELM_KUBEAPISERVER": kube.Server} {
			if value := os.Getenv(key); value != "" && value != expected {
				fail("Kubernetes environment", key+" conflicts with declared target")
			}
		}
		server := kube.Server
		sourceOK := kube.EndpointSource == "" || kube.EndpointSource == "eks" || kube.EndpointSource == "kind"
		if kube.EndpointSource == "eks" && cluster && !accountVerified {
			fail("EKS identity", "expected AWS account was not verified")
			sourceOK = false
		}
		if kube.EndpointSource == "eks" && cluster && accountVerified {
			endpoint, lookupErr := eksEndpoint(ctx, timeout, pathEnv, target, directory)
			if lookupErr != nil {
				fail("EKS identity", "EKS ARN/account/region/endpoint/CA check failed")
				sourceOK = false
			} else {
				server = endpoint
			}
		}
		u, err := url.Parse(server)
		endpointOK := err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
		if kube.EndpointSource == "eks" && !cluster && server == "" {
			endpointOK = target.AWS != nil && validSelector(target.ClusterName)
		}
		if kube.EndpointSource == "kind" {
			if !endpointOK {
				sourceOK = false
			} else {
				ip := net.ParseIP(u.Hostname())
				endpointOK = endpointOK && ip != nil && ip.IsLoopback() && u.Port() != "" && validSelector(target.ClusterName) && kube.Context == "kind-"+target.ClusterName && kube.Cluster == "kind-"+target.ClusterName
			}
		}
		if !validSelector(kube.Context) || !validSelector(kube.Cluster) || !sourceOK || !endpointOK {
			fail("Kubernetes target", "expected context/cluster/endpoint policy is incomplete or invalid")
		} else if cluster {
			out, err := targetOutput(ctx, timeout, pathEnv, target, directory, "kubectl", "config", "view", "--minify", "--output", "json")
			var config struct {
				Current  string `json:"current-context"`
				Contexts []struct {
					Name    string `json:"name"`
					Context struct {
						Cluster string `json:"cluster"`
					} `json:"context"`
				} `json:"contexts"`
				Clusters []struct {
					Name    string `json:"name"`
					Cluster struct {
						Server string `json:"server"`
					} `json:"cluster"`
				} `json:"clusters"`
			}
			ok := err == nil && json.Unmarshal(out, &config) == nil && config.Current == kube.Context && len(config.Contexts) == 1 && len(config.Clusters) == 1
			if ok {
				ok = config.Contexts[0].Name == kube.Context && config.Contexts[0].Context.Cluster == kube.Cluster && config.Clusters[0].Name == kube.Cluster && config.Clusters[0].Cluster.Server == server
			}
			checks = append(checks, checkStatus{Name: "Kubernetes identity", OK: ok, Message: "current context, cluster and server must match the declared target"})
		}
	}
	if target.AWS == nil && target.Kubernetes == nil && target.ClusterName == "" {
		fail("target", "target must declare an identity assertion")
	}
	return checks
}

func targetEnvironment(target *templateengine.Target) map[string]string { return target.Environment() }

// Reject conflicting selectors on direct tools. Trusted shell scripts and
// Terraform provider blocks must honor their declared inputs/scoped environment.
func validateStepTarget(resolved *templateengine.Resolved, step templateengine.PlannedStep) error {
	target := resolved.Platform.Target
	if step.EstablishesClusterTarget && (target == nil || !target.Bootstrap) {
		return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_TARGET_MISMATCH", "establishes_cluster_target requires an explicit bootstrap policy")
	}
	if target == nil {
		return nil
	}
	invalid := func() error {
		return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_TARGET_MISMATCH", "workflow step has an unverified or conflicting target selector")
	}
	eks, configWrite := directEKSStep(step.Command, step.Args)
	if (eks || filepath.Base(step.Command) == "kind") && !clusterNameID.MatchString(target.ClusterName) {
		return invalid()
	}
	if eks {
		// Only a literal cluster selector is supported in a direct EKS step.
		// JSON/YAML input, resource ARNs and files can otherwise select an
		// unrelated resource while the account/region assertion still passes.
		found := false
		for _, arg := range step.Args {
			key, _, _ := strings.Cut(arg, "=")
			if key == "--name" || key == "--cluster-name" {
				found = true
			}
			if strings.HasPrefix(key, "--cli-input") || strings.HasPrefix(key, "--resource-arn") || strings.HasPrefix(key, "--cluster-id") {
				return invalid()
			}
		}
		if !found {
			return invalid()
		}
	}
	if configWrite && (target.Kubernetes == nil || target.Kubernetes.Kubeconfig == "") {
		return invalid()
	}
	expected := map[string]string{}
	switch filepath.Base(step.Command) {
	case "aws":
		if target.AWS != nil {
			expected["--profile"] = target.AWS.Profile
			expected["--region"] = target.AWS.Region
			expected["--endpoint-url"] = ""
			expected["--no-sign-request"] = ""
			expected["--ca-bundle"] = ""
			expected["--no-verify-ssl"] = ""
			expected["--kubeconfig"] = ""
			if configWrite {
				expected["--assume-role-arn"] = ""
				expected["--role-arn"] = ""
				expected["--user-alias"] = ""
				expected["--alias"] = target.Kubernetes.Context
			}
			for _, arg := range step.Args {
				if arg == "eks" && target.ClusterName != "" {
					expected["--name"] = target.ClusterName
					expected["--cluster-name"] = target.ClusterName
					expected["--cli-input-json"] = ""
					expected["--cli-input-yaml"] = ""
					expected["--resource-arn"] = ""
					expected["--cluster-id"] = ""
				}
			}
		}
	case "terraform":
		if target.AWS != nil {
			for key, value := range map[string]string{"aws_profile": target.AWS.Profile, "region": target.AWS.Region} {
				if actual, ok := resolved.Values[key]; !ok || fmt.Sprint(actual) != value {
					return invalid()
				}
			}
			for _, item := range os.Environ() {
				key, value, _ := strings.Cut(item, "=")
				if strings.HasPrefix(key, "TF_CLI_ARGS") && value != "" {
					return invalid()
				}
			}
			expected["-chdir"] = ""
			expected["-var"] = ""
			expected["-var-file"] = ""
		}
	case "kubectl":
		if target.Kubernetes != nil {
			expected["--context"] = target.Kubernetes.Context
			expected["--cluster"] = target.Kubernetes.Cluster
			expected["--server"] = target.Kubernetes.Server
			expected["--kubeconfig"] = ""
			expected["-s"] = target.Kubernetes.Server
		}
	case "helm":
		if target.Kubernetes != nil {
			expected["--kube-context"] = target.Kubernetes.Context
			expected["--kube-apiserver"] = target.Kubernetes.Server
			expected["--kubeconfig"] = ""
		}
	case "kind":
		expected["--kubeconfig"] = ""
		if target.ClusterName != "" {
			found := false
			for _, arg := range step.Args {
				if arg == "--name" || strings.HasPrefix(arg, "--name=") {
					found = true
				}
			}
			if !found {
				return invalid()
			}
			expected["--name"] = target.ClusterName
		}
	}
	for i := 0; i < len(step.Args); i++ {
		key, value, inline := strings.Cut(step.Args[i], "=")
		want, checked := expected[key]
		if !checked && strings.HasPrefix(key, "--") {
			// AWS CLI accepts abbreviated long options. Never allow an alias
			// of a guarded selector to evade the exact-name comparison.
			for selector := range expected {
				if strings.HasPrefix(selector, key) {
					return invalid()
				}
			}
		}
		if !checked && filepath.Base(step.Command) == "kubectl" && target.Kubernetes != nil && strings.HasPrefix(key, "-s") && !strings.HasPrefix(key, "--") {
			want, checked, value, inline = target.Kubernetes.Server, true, strings.TrimPrefix(step.Args[i], "-s"), true
		}
		if !checked {
			continue
		}
		if !inline {
			i++
			if i >= len(step.Args) {
				return invalid()
			}
			value = step.Args[i]
		}
		if want == "" || value != want {
			return invalid()
		}
	}
	return nil
}

func validSelector(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\r\n\x00") && !strings.HasPrefix(value, "-")
}

func targetOutput(ctx context.Context, timeout time.Duration, pathEnv string, target *templateengine.Target, directory string, name string, args ...string) ([]byte, error) {
	path, err := lookPathInEnv(pathEnv, name)
	if err != nil {
		return nil, err
	}
	env := appendPath("PATH", pathEnv)
	for key, value := range targetEnvironment(target) {
		filtered := env[:0]
		for _, item := range env {
			if !strings.HasPrefix(item, key+"=") {
				filtered = append(filtered, item)
			}
		}
		env = append(filtered, key+"="+value)
	}
	return checkexec.RunInDir(ctx, timeout, path, args, env, directory)
}

func requireTarget(ctx context.Context, pathEnv string, resolved *templateengine.Resolved, cluster bool) error {
	return requireTargetAt(ctx, pathEnv, resolved, cluster, "")
}

func requireTargetAt(ctx context.Context, pathEnv string, resolved *templateengine.Resolved, cluster bool, directory string) error {
	for _, check := range targetChecksAt(ctx, 5*time.Second, pathEnv, resolved, cluster, directory) {
		if !check.OK {
			return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_TARGET_MISMATCH", check.Name+": "+check.Message)
		}
	}
	return nil
}

// Custom scripts must use requires_cluster: true. This is a trusted-template
// contract, not command parsing or a sandbox for arbitrary shell programs.
func clusterStep(step templateengine.PlannedStep) bool {
	if step.RequiresCluster {
		return true
	}
	switch filepath.Base(step.Command) {
	case "kubectl":
		return true
	case "helm":
		return len(step.Args) == 0 || (step.Args[0] != "repo" && step.Args[0] != "version")
	}
	return false
}

func phaseNeedsCluster(resolved *templateengine.Resolved, phase string) bool {
	for _, step := range resolved.StepsFor(phase) {
		command, err := resolved.Render(step.Command)
		if err != nil {
			return true
		}
		args := make([]string, 0, len(step.Args))
		for _, arg := range step.Args {
			value, err := resolved.Render(arg)
			if err != nil {
				return true
			}
			args = append(args, value)
		}
		if clusterStep(templateengine.PlannedStep{Command: command, Args: args, RequiresCluster: step.RequiresCluster}) {
			return true
		}
	}
	return false
}
