package toolchain

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if spec.ChecksumURL == "" || spec.ChecksumFile == "" {
		t.Fatalf("terraform checksum metadata missing: %#v", spec)
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
	if spec.ChecksumURL == "" || spec.ChecksumFile == "" {
		t.Fatalf("helm checksum metadata missing: %#v", spec)
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
	if spec.ChecksumURL == "" {
		t.Fatalf("kubectl checksum metadata missing: %#v", spec)
	}
}

func TestExtractChecksumSingleHash(t *testing.T) {
	got, err := extractChecksum("d491f4c47c34856188d38e87a27866bd94a66a57b8db3093a82ae43baf3bb20d\n", "")
	if err != nil {
		t.Fatalf("extractChecksum returned error: %v", err)
	}
	want := "d491f4c47c34856188d38e87a27866bd94a66a57b8db3093a82ae43baf3bb20d"
	if got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}

func TestExtractChecksumWithFilename(t *testing.T) {
	content := "48e30d236a1f334c6acb78501be5a851eaa2a267fefeb1131b6484eb2f9f30d7  helm-v3.18.6-darwin-arm64.tar.gz\n"
	got, err := extractChecksum(content, "helm-v3.18.6-darwin-arm64.tar.gz")
	if err != nil {
		t.Fatalf("extractChecksum returned error: %v", err)
	}
	want := "48e30d236a1f334c6acb78501be5a851eaa2a267fefeb1131b6484eb2f9f30d7"
	if got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}

func TestExtractChecksumNotFound(t *testing.T) {
	_, err := extractChecksum("abcd  file.txt\n", "other.txt")
	if err == nil {
		t.Fatal("extractChecksum succeeded, want error")
	}
}

func TestFileChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.bin")
	if err := os.WriteFile(path, []byte("platformctl"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := fileChecksum(path)
	if err != nil {
		t.Fatalf("fileChecksum returned error: %v", err)
	}
	want := "9d39898946cdcb01d1c9c8404aa743bf4cf79f5d60ec218d843ce817f0f8b8d2"
	if got != want {
		t.Fatalf("checksum = %q, want %q", got, want)
	}
}

func TestVerifyChecksumRejectsMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"))
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "tool.bin")
	if err := os.WriteFile(path, []byte("platformctl"), 0644); err != nil {
		t.Fatal(err)
	}
	err := verifyChecksum(path, spec{ChecksumURL: server.URL})
	if err == nil {
		t.Fatal("verifyChecksum succeeded, want mismatch error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("error = %v, want checksum mismatch", err)
	}
}
