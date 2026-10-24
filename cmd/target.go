package cmd

import (
	"context"
	"encoding/json"
	"fmt"
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

func targetChecks(ctx context.Context, timeout time.Duration, pathEnv string, resolved *templateengine.Resolved, cluster bool) []checkStatus {
	target := resolved.Platform.Target
	if target == nil {
		return nil
	}
	var checks []checkStatus
	fail := func(name, message string) { checks = append(checks, checkStatus{Name: name, Message: message}) }
	if target.ClusterName != "" {
		if value, ok := resolved.Values["cluster_name"]; !validSelector(target.ClusterName) || !ok || fmt.Sprint(value) != target.ClusterName {
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
			out, err := targetOutput(ctx, timeout, pathEnv, target, "aws", args...)
			var identity struct {
				Account string `json:"Account"`
			}
			if err != nil || json.Unmarshal(out, &identity) != nil || identity.Account != aws.Account {
				fail("AWS account", "AWS identity check failed or returned a different account")
			} else {
				checks = append(checks, checkStatus{Name: "AWS account", OK: true})
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
		u, err := url.Parse(kube.Server)
		if !validSelector(kube.Context) || !validSelector(kube.Cluster) || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			fail("Kubernetes target", "target.kubernetes requires context, cluster and HTTPS server without credentials")
		} else if cluster {
			out, err := targetOutput(ctx, timeout, pathEnv, target, "kubectl", "config", "view", "--minify", "--output", "json")
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
				ok = config.Contexts[0].Name == kube.Context && config.Contexts[0].Context.Cluster == kube.Cluster && config.Clusters[0].Name == kube.Cluster && config.Clusters[0].Cluster.Server == kube.Server
			}
			checks = append(checks, checkStatus{Name: "Kubernetes identity", OK: ok, Message: "current context, cluster and server must match the declared target"})
		}
	}
	if target.AWS == nil && target.Kubernetes == nil && target.ClusterName == "" {
		fail("target", "target must declare an identity assertion")
	}
	return checks
}

func targetEnvironment(target *templateengine.Target) map[string]string {
	if target == nil || target.AWS == nil {
		return nil
	}
	// Named profile credentials and region are the same for STS and workflow
	// children. Inherited static/web-identity credentials cannot select account B.
	return map[string]string{
		"AWS_PROFILE": target.AWS.Profile, "AWS_DEFAULT_PROFILE": target.AWS.Profile,
		"AWS_REGION": target.AWS.Region, "AWS_DEFAULT_REGION": target.AWS.Region,
		"AWS_ACCESS_KEY_ID": "", "AWS_SECRET_ACCESS_KEY": "", "AWS_SESSION_TOKEN": "", "AWS_SECURITY_TOKEN": "",
		"AWS_WEB_IDENTITY_TOKEN_FILE": "", "AWS_ROLE_ARN": "", "AWS_ROLE_SESSION_NAME": "",
	}
}

// Reject conflicting selectors on direct tools. Trusted shell scripts and
// Terraform provider blocks must honor their declared inputs/scoped environment.
func validateStepTarget(resolved *templateengine.Resolved, step templateengine.PlannedStep) error {
	target := resolved.Platform.Target
	if target == nil {
		return nil
	}
	invalid := func() error {
		return apperror.New(apperror.CategoryDependency, "PLATFORMCTL_TARGET_MISMATCH", "workflow step has an unverified or conflicting target selector")
	}
	expected := map[string]string{}
	switch filepath.Base(step.Command) {
	case "aws":
		if target.AWS != nil {
			expected["--profile"] = target.AWS.Profile
			expected["--region"] = target.AWS.Region
			expected["--endpoint-url"] = ""
			expected["--no-sign-request"] = ""
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
		if target.ClusterName != "" {
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

func targetOutput(ctx context.Context, timeout time.Duration, pathEnv string, target *templateengine.Target, name string, args ...string) ([]byte, error) {
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
	return checkexec.Run(ctx, timeout, path, args, env)
}

func requireTarget(ctx context.Context, pathEnv string, resolved *templateengine.Resolved, cluster bool) error {
	for _, check := range targetChecks(ctx, 5*time.Second, pathEnv, resolved, cluster) {
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
