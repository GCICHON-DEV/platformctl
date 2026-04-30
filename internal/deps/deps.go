package deps

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Tool struct {
	Name        string   `json:"name"`
	BrewPackage string   `json:"brew_package,omitempty"`
	VersionArgs []string `json:"version_args,omitempty"`
	RequiredFor string   `json:"required_for,omitempty"`
	MinVersion  string   `json:"min_version,omitempty"`
	Version     string   `json:"version,omitempty"`
	PathEnv     string   `json:"-"`
}

type Status struct {
	Tool      Tool   `json:"tool"`
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
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
		statuses = append(statuses, CheckOne(tool))
	}
	return statuses
}

func CheckOne(tool Tool) Status {
	path, err := lookPath(tool)
	status := Status{Tool: tool, Installed: err == nil, Path: path}
	if status.Installed {
		status.Version = version(tool)
	}
	return status
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

func VersionProblem(status Status) string {
	if !status.Installed || status.Version == "" {
		return ""
	}
	if status.Tool.Version != "" {
		ok, err := versionMatches(status.Version, status.Tool.Version)
		if err != nil {
			return fmt.Sprintf("%s version %q could not be compared with pinned version %s", status.Tool.Name, status.Version, status.Tool.Version)
		}
		if !ok {
			return fmt.Sprintf("%s version %q does not match pinned version %s", status.Tool.Name, status.Version, status.Tool.Version)
		}
	}
	if status.Tool.MinVersion == "" || status.Version == "" {
		return ""
	}
	ok, err := versionAtLeast(status.Version, status.Tool.MinVersion)
	if err != nil {
		return fmt.Sprintf("%s version %q could not be compared with required minimum %s", status.Tool.Name, status.Version, status.Tool.MinVersion)
	}
	if !ok {
		return fmt.Sprintf("%s version %q is lower than required minimum %s", status.Tool.Name, status.Version, status.Tool.MinVersion)
	}
	return ""
}

func versionMatches(actualText, pinnedText string) (bool, error) {
	actual, err := parseVersion(actualText)
	if err != nil {
		return false, err
	}
	pinned, err := parseVersion(pinnedText)
	if err != nil {
		return false, err
	}
	return actual == pinned, nil
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

	path, err := lookPath(tool)
	if err != nil {
		return ""
	}
	cmd := exec.CommandContext(ctx, path, tool.VersionArgs...)
	if tool.PathEnv != "" {
		cmd.Env = appendEnvPath(tool.PathEnv)
	}
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

func lookPath(tool Tool) (string, error) {
	if tool.PathEnv == "" {
		return exec.LookPath(tool.Name)
	}
	for _, dir := range filepath.SplitList(tool.PathEnv) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, tool.Name)
		if runtime.GOOS == "windows" && !strings.HasSuffix(candidate, ".exe") {
			candidate += ".exe"
		}
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() && stat.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

func appendEnvPath(path string) []string {
	env := os.Environ()
	for i, item := range env {
		if strings.HasPrefix(item, "PATH=") {
			env[i] = "PATH=" + path
			return env
		}
	}
	return append(env, "PATH="+path)
}

var versionNumberPattern = regexp.MustCompile(`[0-9]+(\.[0-9]+){0,2}`)

func versionAtLeast(actualText, minimumText string) (bool, error) {
	actual, err := parseVersion(actualText)
	if err != nil {
		return false, err
	}
	minimum, err := parseVersion(minimumText)
	if err != nil {
		return false, err
	}
	for i := 0; i < len(minimum); i++ {
		if actual[i] > minimum[i] {
			return true, nil
		}
		if actual[i] < minimum[i] {
			return false, nil
		}
	}
	return true, nil
}

func parseVersion(text string) ([3]int, error) {
	var out [3]int
	raw := versionNumberPattern.FindString(text)
	if raw == "" {
		return out, fmt.Errorf("no version number found")
	}
	parts := strings.Split(raw, ".")
	for i := 0; i < len(parts) && i < len(out); i++ {
		value, err := strconv.Atoi(parts[i])
		if err != nil {
			return out, err
		}
		out[i] = value
	}
	return out, nil
}
