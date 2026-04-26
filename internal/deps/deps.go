package deps

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type Tool struct {
	Name        string
	BrewPackage string
	VersionArgs []string
	RequiredFor string
}

type Status struct {
	Tool      Tool
	Installed bool
	Path      string
	Version   string
}

func RuntimeTools() []Tool {
	return []Tool{
		{Name: "terraform", BrewPackage: "hashicorp/tap/terraform", VersionArgs: []string{"version"}, RequiredFor: "AWS infrastructure provisioning"},
		{Name: "aws", BrewPackage: "awscli", VersionArgs: []string{"--version"}, RequiredFor: "EKS kubeconfig and AWS API access"},
		{Name: "kubectl", BrewPackage: "kubectl", VersionArgs: []string{"version", "--client"}, RequiredFor: "Kubernetes manifest apply and cluster checks"},
		{Name: "helm", BrewPackage: "helm", VersionArgs: []string{"version", "--short"}, RequiredFor: "ArgoCD, monitoring, and ingress installation"},
	}
}

func DevTools() []Tool {
	return []Tool{
		{Name: "go", BrewPackage: "go", VersionArgs: []string{"version"}, RequiredFor: "building platformctl from source"},
	}
}

func Check(tools []Tool) []Status {
	statuses := make([]Status, 0, len(tools))
	for _, tool := range tools {
		path, err := exec.LookPath(tool.Name)
		status := Status{Tool: tool, Installed: err == nil, Path: path}
		if status.Installed {
			status.Version = version(tool)
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func Missing(statuses []Status) []Tool {
	var missing []Tool
	for _, status := range statuses {
		if !status.Installed {
			missing = append(missing, status.Tool)
		}
	}
	return missing
}

func InstallMissing(tools []Tool, stdout, stderr io.Writer) error {
	if len(tools) == 0 {
		fmt.Fprintln(stdout, "All dependencies are already installed.")
		return nil
	}

	if runtime.GOOS != "darwin" {
		return fmt.Errorf("automatic installation is currently supported only on macOS with Homebrew\n\n%s", ManualInstallInstructions(tools))
	}

	if _, err := exec.LookPath("brew"); err != nil {
		return fmt.Errorf("Homebrew is required for automatic installation on macOS: https://brew.sh\n\n%s", ManualInstallInstructions(tools))
	}

	for _, tool := range tools {
		fmt.Fprintf(stdout, "\n==> installing %s\n", tool.Name)
		cmd := exec.Command("brew", "install", tool.BrewPackage)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("install %s with Homebrew: %w", tool.Name, err)
		}
	}
	return nil
}

func ManualInstallInstructions(tools []Tool) string {
	var b strings.Builder
	b.WriteString("Install missing dependencies manually:\n")
	for _, tool := range tools {
		switch runtime.GOOS {
		case "darwin":
			fmt.Fprintf(&b, "  brew install %s\n", tool.BrewPackage)
		default:
			fmt.Fprintf(&b, "  %s: install from the official documentation or your OS package manager\n", tool.Name)
		}
	}
	return strings.TrimSpace(b.String())
}

func version(tool Tool) string {
	if len(tool.VersionArgs) == 0 {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, tool.Name, tool.VersionArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
