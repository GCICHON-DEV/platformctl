package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"platformctl/internal/templateengine"
)

// Read only the explicitly scoped kubeconfig; never print its user credentials.
func scopedClusterCA(target *templateengine.Target) (string, []byte, error) {
	paths := filepath.SplitList(target.Environment()["KUBECONFIG"])
	if len(paths) != 1 {
		return "", nil, fmt.Errorf("one explicit kubeconfig is required for endpoint verification")
	}
	data, err := templateengine.ReadTargetFile(paths[0], 128*1024)
	if err != nil {
		return "", nil, fmt.Errorf("kubeconfig must be a bounded regular file")
	}

	var config struct {
		Clusters []struct {
			Name    string `yaml:"name"`
			Cluster struct {
				Server   string `yaml:"server"`
				Data     string `yaml:"certificate-authority-data"`
				File     string `yaml:"certificate-authority"`
				Insecure bool   `yaml:"insecure-skip-tls-verify"`
			} `yaml:"cluster"`
		} `yaml:"clusters"`
	}
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", nil, fmt.Errorf("invalid scoped kubeconfig")
	}
	var server string
	var ca []byte
	found := 0
	for _, entry := range config.Clusters {
		if entry.Name != target.Kubernetes.Cluster {
			continue
		}
		found++
		if entry.Cluster.Insecure || entry.Cluster.File != "" {
			return "", nil, fmt.Errorf("inline CA and TLS verification are required")
		}
		server = entry.Cluster.Server
		ca, err = base64.StdEncoding.DecodeString(entry.Cluster.Data)
		if err != nil || len(ca) == 0 {
			return "", nil, fmt.Errorf("inline CA is invalid or empty")
		}
	}
	if found != 1 {
		return "", nil, fmt.Errorf("expected cluster must occur exactly once")
	}
	return server, ca, nil
}

func eksEndpoint(ctx context.Context, timeout time.Duration, pathEnv string, target *templateengine.Target, directory string) (string, error) {
	if target.AWS == nil || !validSelector(target.ClusterName) {
		return "", fmt.Errorf("EKS requires expected AWS identity and cluster_name")
	}
	aws := target.AWS
	out, err := targetOutput(ctx, timeout, pathEnv, target, directory, "aws", "eks", "describe-cluster", "--name", target.ClusterName, "--profile", aws.Profile, "--region", aws.Region, "--output", "json")
	if err != nil {
		return "", err
	}
	var response struct {
		Cluster struct {
			Name     string `json:"name"`
			ARN      string `json:"arn"`
			Status   string `json:"status"`
			Endpoint string `json:"endpoint"`
			CA       struct {
				Data string `json:"data"`
			} `json:"certificateAuthority"`
		} `json:"cluster"`
	}
	if json.Unmarshal(out, &response) != nil {
		return "", fmt.Errorf("invalid EKS identity response")
	}
	c := response.Cluster
	arn := strings.SplitN(c.ARN, ":", 6)
	partition := "aws"
	if strings.HasPrefix(aws.Region, "cn-") {
		partition = "aws-cn"
	}
	if strings.HasPrefix(aws.Region, "us-gov-") {
		partition = "aws-us-gov"
	}
	if len(arn) != 6 || arn[0] != "arn" || arn[1] != partition || arn[2] != "eks" || arn[3] != aws.Region || arn[4] != aws.Account || arn[5] != "cluster/"+target.ClusterName || c.Name != target.ClusterName || c.Status != "ACTIVE" || c.ARN != target.Kubernetes.Cluster {
		return "", fmt.Errorf("EKS cluster ARN/account/region/name/status mismatch")
	}
	server, actualCA, err := scopedClusterCA(target)
	if err != nil {
		return "", err
	}
	expectedCA, err := base64.StdEncoding.DecodeString(c.CA.Data)
	if err != nil || len(expectedCA) == 0 || !bytes.Equal(actualCA, expectedCA) || c.Endpoint != server || (target.Kubernetes.Server != "" && target.Kubernetes.Server != server) {
		return "", fmt.Errorf("EKS endpoint or certificate authority mismatch")
	}
	return server, nil
}
