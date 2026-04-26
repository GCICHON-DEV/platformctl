package config

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

var kubernetesVersionPattern = regexp.MustCompile(`^1\.[0-9]+$`)

func (c *Config) Validate() error {
	var problems []string

	require(&problems, c.Project.Name, "project.name is required")
	require(&problems, c.Project.EnvironmentPrefix, "project.environment_prefix is required")

	if c.Provider.Name != "aws" {
		problems = append(problems, "provider.name must be aws")
	}
	require(&problems, c.Provider.Region, "provider.region is required")

	if c.Terraform.Backend.Type != "local" {
		problems = append(problems, "terraform.backend.type must be local for the MVP")
	}

	require(&problems, c.Cluster.Name, "cluster.name is required")
	if !kubernetesVersionPattern.MatchString(c.Cluster.Version) {
		problems = append(problems, "cluster.version must use the format 1.x, for example 1.34")
	}
	if _, _, err := net.ParseCIDR(c.Cluster.VPC.CIDR); err != nil {
		problems = append(problems, fmt.Sprintf("cluster.vpc.cidr is invalid: %v", err))
	}
	if c.Cluster.VPC.AZCount < 2 {
		problems = append(problems, "cluster.vpc.az_count must be at least 2")
	}
	if len(c.Cluster.NodeGroups) == 0 {
		problems = append(problems, "cluster.node_groups must contain at least one node group")
	}
	validateNodeGroups(&problems, c.Cluster.NodeGroups)
	validateEnvironments(&problems, c.Environments)
	validateGitOps(&problems, c.GitOps)
	validateMonitoring(&problems, c.Monitoring)
	validateIngress(&problems, c.Ingress)
	validateAddons(&problems, c.Addons)

	if len(problems) > 0 {
		return errors.New("invalid configuration:\n- " + strings.Join(problems, "\n- "))
	}
	return nil
}

func require(problems *[]string, value, message string) {
	if strings.TrimSpace(value) == "" {
		*problems = append(*problems, message)
	}
}

func validateNodeGroups(problems *[]string, groups []NodeGroup) {
	names := map[string]bool{}
	for i, group := range groups {
		prefix := fmt.Sprintf("cluster.node_groups[%d]", i)
		require(problems, group.Name, prefix+".name is required")
		require(problems, group.InstanceType, prefix+".instance_type is required")
		if names[group.Name] {
			*problems = append(*problems, prefix+".name must be unique")
		}
		names[group.Name] = true
		if group.MinCapacity < 1 {
			*problems = append(*problems, prefix+".min_capacity must be at least 1")
		}
		if group.DesiredCapacity < group.MinCapacity {
			*problems = append(*problems, prefix+".desired_capacity must be greater than or equal to min_capacity")
		}
		if group.MaxCapacity < group.DesiredCapacity {
			*problems = append(*problems, prefix+".max_capacity must be greater than or equal to desired_capacity")
		}
	}
}

func validateEnvironments(problems *[]string, envs []Environment) {
	if len(envs) == 0 {
		*problems = append(*problems, "environments must contain at least one environment")
		return
	}

	names := map[string]bool{}
	namespaces := map[string]bool{}
	for i, env := range envs {
		prefix := fmt.Sprintf("environments[%d]", i)
		require(problems, env.Name, prefix+".name is required")
		require(problems, env.Namespace, prefix+".namespace is required")
		require(problems, env.ResourceQuota.CPU, prefix+".resource_quota.cpu is required")
		require(problems, env.ResourceQuota.Memory, prefix+".resource_quota.memory is required")
		require(problems, env.LimitRange.DefaultCPU, prefix+".limit_range.default_cpu is required")
		require(problems, env.LimitRange.DefaultMemory, prefix+".limit_range.default_memory is required")
		if names[env.Name] {
			*problems = append(*problems, prefix+".name must be unique")
		}
		if namespaces[env.Namespace] {
			*problems = append(*problems, prefix+".namespace must be unique")
		}
		names[env.Name] = true
		namespaces[env.Namespace] = true
	}
}

func validateGitOps(problems *[]string, gitops GitOpsConfig) {
	if !gitops.Enabled {
		return
	}
	if gitops.Tool != "argocd" {
		*problems = append(*problems, "gitops.tool must be argocd when gitops.enabled is true")
	}
	require(problems, gitops.Namespace, "gitops.namespace is required when gitops.enabled is true")
}

func validateMonitoring(problems *[]string, monitoring MonitoringConfig) {
	if !monitoring.Enabled {
		return
	}
	if monitoring.Stack != "kube-prometheus-stack" {
		*problems = append(*problems, "monitoring.stack must be kube-prometheus-stack when monitoring.enabled is true")
	}
	require(problems, monitoring.Namespace, "monitoring.namespace is required when monitoring.enabled is true")
	require(problems, monitoring.Grafana.AdminUser, "monitoring.grafana.admin_user is required when monitoring.enabled is true")
}

func validateIngress(problems *[]string, ingress IngressConfig) {
	if !ingress.Enabled {
		return
	}
	if ingress.Controller != "nginx" {
		*problems = append(*problems, "ingress.controller must be nginx for the MVP")
	}
	require(problems, ingress.Namespace, "ingress.namespace is required when ingress.enabled is true")
}

func validateAddons(problems *[]string, addons AddonsConfig) {
	if addons.CertManager.Enabled {
		*problems = append(*problems, "addons.cert_manager.enabled=true is a placeholder and is not implemented in the MVP")
	}
	if addons.ExternalDNS.Enabled {
		*problems = append(*problems, "addons.external_dns.enabled=true is a placeholder and is not implemented in the MVP")
	}
	if addons.AWSLoadBalancerController.Enabled {
		*problems = append(*problems, "addons.aws_load_balancer_controller.enabled=true is a placeholder and is not implemented in the MVP")
	}
}
