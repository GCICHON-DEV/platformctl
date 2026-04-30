package toolchain

import (
	"strings"
	"testing"

	"platformctl/internal/templateengine"
)

func TestDownloadSpecTerraform(t *testing.T) {
	spec, err := downloadSpec(templateengine.Tool{Name: "terraform", Version: "1.5.7"})
	if err != nil {
		t.Fatalf("downloadSpec returned error: %v", err)
	}
	if !strings.Contains(spec.URL, "https://releases.hashicorp.com/terraform/1.5.7/terraform_1.5.7_") {
		t.Fatalf("unexpected terraform URL: %s", spec.URL)
	}
	if spec.Format != "zip" {
		t.Fatalf("format = %q, want zip", spec.Format)
	}
}

func TestDownloadSpecHelm(t *testing.T) {
	spec, err := downloadSpec(templateengine.Tool{Name: "helm", Version: "v3.18.6"})
	if err != nil {
		t.Fatalf("downloadSpec returned error: %v", err)
	}
	if !strings.HasPrefix(spec.URL, "https://get.helm.sh/helm-v3.18.6-") {
		t.Fatalf("unexpected helm URL: %s", spec.URL)
	}
	if spec.Format != "tar.gz" {
		t.Fatalf("format = %q, want tar.gz", spec.Format)
	}
}

func TestDownloadSpecKubectl(t *testing.T) {
	spec, err := downloadSpec(templateengine.Tool{Name: "kubectl", Version: "v1.34.0"})
	if err != nil {
		t.Fatalf("downloadSpec returned error: %v", err)
	}
	if !strings.HasPrefix(spec.URL, "https://dl.k8s.io/release/v1.34.0/bin/") {
		t.Fatalf("unexpected kubectl URL: %s", spec.URL)
	}
	if spec.Format != "binary" {
		t.Fatalf("format = %q, want binary", spec.Format)
	}
}
