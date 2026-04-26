package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type PlatformFile struct {
	Template TemplateSource `yaml:"template"`
	Values   TemplateValues `yaml:"values"`
}

type TemplateSource struct {
	Source  string `yaml:"source"`
	Version string `yaml:"version"`
}

type TemplateDefinition struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Defaults    TemplateValues `yaml:"defaults"`
}

type TemplateValues struct {
	ProjectName            string        `yaml:"project_name"`
	EnvironmentPrefix      string        `yaml:"environment_prefix"`
	AWSRegion              string        `yaml:"aws_region"`
	AWSProfile             string        `yaml:"aws_profile"`
	ClusterName            string        `yaml:"cluster_name"`
	KubernetesVersion      string        `yaml:"kubernetes_version"`
	VPCCIDR                string        `yaml:"vpc_cidr"`
	AZCount                int           `yaml:"az_count"`
	NodeGroupName          string        `yaml:"node_group_name"`
	NodeInstanceType       string        `yaml:"node_instance_type"`
	NodeDesiredCapacity    int           `yaml:"node_desired_capacity"`
	NodeMinCapacity        int           `yaml:"node_min_capacity"`
	NodeMaxCapacity        int           `yaml:"node_max_capacity"`
	Environments           []Environment `yaml:"environments"`
	ArgoCDEnabled          *bool         `yaml:"argocd_enabled"`
	ArgoCDNamespace        string        `yaml:"argocd_namespace"`
	MonitoringEnabled      *bool         `yaml:"monitoring_enabled"`
	MonitoringNamespace    string        `yaml:"monitoring_namespace"`
	GrafanaAdminUser       string        `yaml:"grafana_admin_user"`
	IngressEnabled         *bool         `yaml:"ingress_enabled"`
	IngressController      string        `yaml:"ingress_controller"`
	IngressNamespace       string        `yaml:"ingress_namespace"`
	CertManagerEnabled     *bool         `yaml:"cert_manager_enabled"`
	ExternalDNSEnabled     *bool         `yaml:"external_dns_enabled"`
	AWSLoadBalancerEnabled *bool         `yaml:"aws_load_balancer_controller_enabled"`
}

type Config struct {
	Template     string           `yaml:"template"`
	Project      ProjectConfig    `yaml:"project"`
	Provider     ProviderConfig   `yaml:"provider"`
	Terraform    TerraformConfig  `yaml:"terraform"`
	Cluster      ClusterConfig    `yaml:"cluster"`
	Environments []Environment    `yaml:"environments"`
	GitOps       GitOpsConfig     `yaml:"gitops"`
	Monitoring   MonitoringConfig `yaml:"monitoring"`
	Ingress      IngressConfig    `yaml:"ingress"`
	Addons       AddonsConfig     `yaml:"addons"`
}

type ProjectConfig struct {
	Name              string `yaml:"name"`
	EnvironmentPrefix string `yaml:"environment_prefix"`
}

type ProviderConfig struct {
	Name    string `yaml:"name"`
	Region  string `yaml:"region"`
	Profile string `yaml:"profile"`
}

type TerraformConfig struct {
	Backend TerraformBackend `yaml:"backend"`
}

type TerraformBackend struct {
	Type string `yaml:"type"`
}

type ClusterConfig struct {
	Name       string      `yaml:"name"`
	Version    string      `yaml:"version"`
	VPC        VPCConfig   `yaml:"vpc"`
	NodeGroups []NodeGroup `yaml:"node_groups"`
}

type VPCConfig struct {
	CIDR    string `yaml:"cidr"`
	AZCount int    `yaml:"az_count"`
}

type NodeGroup struct {
	Name            string `yaml:"name"`
	InstanceType    string `yaml:"instance_type"`
	DesiredCapacity int    `yaml:"desired_capacity"`
	MinCapacity     int    `yaml:"min_capacity"`
	MaxCapacity     int    `yaml:"max_capacity"`
}

type Environment struct {
	Name          string        `yaml:"name"`
	Namespace     string        `yaml:"namespace"`
	ResourceQuota ResourceQuota `yaml:"resource_quota"`
	LimitRange    LimitRange    `yaml:"limit_range"`
}

type ResourceQuota struct {
	CPU    string `yaml:"cpu"`
	Memory string `yaml:"memory"`
}

type LimitRange struct {
	DefaultCPU    string `yaml:"default_cpu"`
	DefaultMemory string `yaml:"default_memory"`
}

type GitOpsConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Tool      string `yaml:"tool"`
	Namespace string `yaml:"namespace"`
}

type MonitoringConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Namespace string        `yaml:"namespace"`
	Stack     string        `yaml:"stack"`
	Grafana   GrafanaConfig `yaml:"grafana"`
}

type GrafanaConfig struct {
	AdminUser string `yaml:"admin_user"`
}

type IngressConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Controller string `yaml:"controller"`
	Namespace  string `yaml:"namespace"`
}

type AddonsConfig struct {
	CertManager               AddonToggle `yaml:"cert_manager"`
	ExternalDNS               AddonToggle `yaml:"external_dns"`
	AWSLoadBalancerController AddonToggle `yaml:"aws_load_balancer_controller"`
}

type AddonToggle struct {
	Enabled bool `yaml:"enabled"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var platform PlatformFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&platform); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if platform.Template.Source == "" {
		return nil, fmt.Errorf("platform.yaml must define template.source")
	}

	definition, err := LoadTemplate(platform.Template)
	if err != nil {
		return nil, err
	}

	values := MergeValues(definition.Defaults, platform.Values)
	return values.ToConfig(definition.Name), nil
}

func LoadTemplate(template TemplateSource) (*TemplateDefinition, error) {
	resolved, err := ResolveTemplateSource(template)
	if err != nil {
		return nil, err
	}

	data, err := fetchTemplate(resolved)
	if err != nil {
		return nil, err
	}

	var definition TemplateDefinition
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("parse template from %s: %w", resolved, err)
	}
	if definition.Name == "" {
		return nil, fmt.Errorf("template from %s must define name", resolved)
	}
	return &definition, nil
}

func ResolveTemplateSource(template TemplateSource) (string, error) {
	source := strings.TrimSpace(template.Source)
	version := strings.TrimSpace(template.Version)

	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		return source, nil
	}
	if version == "" {
		return "", fmt.Errorf("template.version is required when template.source is a registry reference")
	}

	parts := strings.Split(source, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("template.source must be an http(s) URL or registry reference like platformctl/aws-eks-standard")
	}

	switch parts[0] {
	case "platformctl":
		baseURL := strings.TrimRight(os.Getenv("PLATFORMCTL_TEMPLATE_REGISTRY_URL"), "/")
		if baseURL == "" {
			baseURL = "https://raw.githubusercontent.com/GCICHON-DEV/platformctl-templates"
		}
		return fmt.Sprintf("%s/%s/%s.yaml", baseURL, version, parts[1]), nil
	default:
		return "", fmt.Errorf("unsupported template registry namespace %q", parts[0])
	}
}

func fetchTemplate(source string) ([]byte, error) {
	if !strings.HasPrefix(source, "https://") && !strings.HasPrefix(source, "http://") {
		return nil, fmt.Errorf("template.source must be an http or https URL")
	}

	cachePath, err := templateCachePath(source)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get(source)
	if err != nil {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("download template %s: %w", source, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			return cached, nil
		}
		return nil, fmt.Errorf("download template %s: unexpected status %s", source, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read template response %s: %w", source, err)
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return nil, fmt.Errorf("create template cache: %w", err)
	}
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return nil, fmt.Errorf("write template cache: %w", err)
	}

	return data, nil
}

func templateCachePath(source string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory for template cache: %w", err)
	}
	sum := sha256.Sum256([]byte(source))
	return filepath.Join(home, ".platformctl", "templates", hex.EncodeToString(sum[:])+".yaml"), nil
}

func MergeValues(base, override TemplateValues) TemplateValues {
	out := base
	if override.ProjectName != "" {
		out.ProjectName = override.ProjectName
	}
	if override.EnvironmentPrefix != "" {
		out.EnvironmentPrefix = override.EnvironmentPrefix
	}
	if override.AWSRegion != "" {
		out.AWSRegion = override.AWSRegion
	}
	if override.AWSProfile != "" {
		out.AWSProfile = override.AWSProfile
	}
	if override.ClusterName != "" {
		out.ClusterName = override.ClusterName
	}
	if override.KubernetesVersion != "" {
		out.KubernetesVersion = override.KubernetesVersion
	}
	if override.VPCCIDR != "" {
		out.VPCCIDR = override.VPCCIDR
	}
	if override.AZCount != 0 {
		out.AZCount = override.AZCount
	}
	if override.NodeGroupName != "" {
		out.NodeGroupName = override.NodeGroupName
	}
	if override.NodeInstanceType != "" {
		out.NodeInstanceType = override.NodeInstanceType
	}
	if override.NodeDesiredCapacity != 0 {
		out.NodeDesiredCapacity = override.NodeDesiredCapacity
	}
	if override.NodeMinCapacity != 0 {
		out.NodeMinCapacity = override.NodeMinCapacity
	}
	if override.NodeMaxCapacity != 0 {
		out.NodeMaxCapacity = override.NodeMaxCapacity
	}
	if len(override.Environments) > 0 {
		out.Environments = override.Environments
	}
	if override.ArgoCDEnabled != nil {
		out.ArgoCDEnabled = override.ArgoCDEnabled
	}
	if override.ArgoCDNamespace != "" {
		out.ArgoCDNamespace = override.ArgoCDNamespace
	}
	if override.MonitoringEnabled != nil {
		out.MonitoringEnabled = override.MonitoringEnabled
	}
	if override.MonitoringNamespace != "" {
		out.MonitoringNamespace = override.MonitoringNamespace
	}
	if override.GrafanaAdminUser != "" {
		out.GrafanaAdminUser = override.GrafanaAdminUser
	}
	if override.IngressEnabled != nil {
		out.IngressEnabled = override.IngressEnabled
	}
	if override.IngressController != "" {
		out.IngressController = override.IngressController
	}
	if override.IngressNamespace != "" {
		out.IngressNamespace = override.IngressNamespace
	}
	if override.CertManagerEnabled != nil {
		out.CertManagerEnabled = override.CertManagerEnabled
	}
	if override.ExternalDNSEnabled != nil {
		out.ExternalDNSEnabled = override.ExternalDNSEnabled
	}
	if override.AWSLoadBalancerEnabled != nil {
		out.AWSLoadBalancerEnabled = override.AWSLoadBalancerEnabled
	}
	return out
}

func (v TemplateValues) ToConfig(templateName string) *Config {
	return &Config{
		Template: templateName,
		Project: ProjectConfig{
			Name:              v.ProjectName,
			EnvironmentPrefix: v.EnvironmentPrefix,
		},
		Provider: ProviderConfig{
			Name:    "aws",
			Region:  v.AWSRegion,
			Profile: v.AWSProfile,
		},
		Terraform: TerraformConfig{
			Backend: TerraformBackend{Type: "local"},
		},
		Cluster: ClusterConfig{
			Name:    v.ClusterName,
			Version: v.KubernetesVersion,
			VPC: VPCConfig{
				CIDR:    v.VPCCIDR,
				AZCount: v.AZCount,
			},
			NodeGroups: []NodeGroup{
				{
					Name:            v.NodeGroupName,
					InstanceType:    v.NodeInstanceType,
					DesiredCapacity: v.NodeDesiredCapacity,
					MinCapacity:     v.NodeMinCapacity,
					MaxCapacity:     v.NodeMaxCapacity,
				},
			},
		},
		Environments: v.Environments,
		GitOps: GitOpsConfig{
			Enabled:   boolValue(v.ArgoCDEnabled),
			Tool:      "argocd",
			Namespace: v.ArgoCDNamespace,
		},
		Monitoring: MonitoringConfig{
			Enabled:   boolValue(v.MonitoringEnabled),
			Namespace: v.MonitoringNamespace,
			Stack:     "kube-prometheus-stack",
			Grafana: GrafanaConfig{
				AdminUser: v.GrafanaAdminUser,
			},
		},
		Ingress: IngressConfig{
			Enabled:    boolValue(v.IngressEnabled),
			Controller: v.IngressController,
			Namespace:  v.IngressNamespace,
		},
		Addons: AddonsConfig{
			CertManager:               AddonToggle{Enabled: boolValue(v.CertManagerEnabled)},
			ExternalDNS:               AddonToggle{Enabled: boolValue(v.ExternalDNSEnabled)},
			AWSLoadBalancerController: AddonToggle{Enabled: boolValue(v.AWSLoadBalancerEnabled)},
		},
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}
