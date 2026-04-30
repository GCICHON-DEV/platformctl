package toolchain

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"platformctl/internal/deps"
	"platformctl/internal/templateengine"
)

type Manager struct {
	BinDir string
	Stdout io.Writer
	Stderr io.Writer
}

func New(stdout, stderr io.Writer) (*Manager, error) {
	dir, err := BinDir()
	if err != nil {
		return nil, err
	}
	return &Manager{BinDir: dir, Stdout: stdout, Stderr: stderr}, nil
}

func BinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory for toolchain: %w", err)
	}
	return filepath.Join(home, ".platformctl", "bin"), nil
}

func (m *Manager) PathEnv() string {
	return m.BinDir + string(os.PathListSeparator) + os.Getenv("PATH")
}

func (m *Manager) Resolve(name string) (string, error) {
	managed := filepath.Join(m.BinDir, executableName(name))
	if stat, err := os.Stat(managed); err == nil && !stat.IsDir() {
		return managed, nil
	}
	return exec.LookPath(name)
}

func (m *Manager) Ensure(tools []templateengine.Tool) ([]deps.Status, error) {
	if err := os.MkdirAll(m.BinDir, 0755); err != nil {
		return nil, fmt.Errorf("create toolchain directory: %w", err)
	}
	statuses := make([]deps.Status, 0, len(tools))
	for _, tool := range tools {
		depTool := deps.Tool{
			Name:        tool.Name,
			VersionArgs: defaultVersionArgs(tool),
			RequiredFor: "platform template",
			MinVersion:  tool.MinVersion,
			Version:     desiredVersion(tool),
			PathEnv:     m.PathEnv(),
		}
		if canInstall(tool.Name) && desiredVersion(tool) != "" {
			managedTool := depTool
			managedTool.PathEnv = m.BinDir
			status := deps.CheckOne(managedTool)
			if status.Installed && deps.VersionProblem(status) == "" {
				statuses = append(statuses, status)
				continue
			}
			fmt.Fprintf(m.Stdout, "[install] %-10s %s -> %s\n", tool.Name, desiredVersion(tool), m.BinDir)
			if err := m.install(tool); err != nil {
				if m.Stderr != nil {
					fmt.Fprintf(m.Stderr, "[warn] install %s failed: %v\n", tool.Name, err)
				}
			}
			statuses = append(statuses, deps.CheckOne(depTool))
			continue
		}
		status := deps.CheckOne(depTool)
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (m *Manager) install(tool templateengine.Tool) error {
	spec, err := downloadSpec(tool)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "platformctl-tool-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, filepath.Base(spec.URL))
	if err := download(spec.URL, archivePath); err != nil {
		return err
	}
	target := filepath.Join(m.BinDir, executableName(tool.Name))
	switch spec.Format {
	case "zip":
		return extractZipBinary(archivePath, spec.BinaryPath, target)
	case "tar.gz":
		return extractTarGzBinary(archivePath, spec.BinaryPath, target)
	case "binary":
		return installBinary(archivePath, target)
	default:
		return fmt.Errorf("unsupported archive format %q", spec.Format)
	}
}

type spec struct {
	URL        string
	Format     string
	BinaryPath string
}

func downloadSpec(tool templateengine.Tool) (spec, error) {
	version := desiredVersion(tool)
	if version == "" {
		return spec{}, fmt.Errorf("%s requires tool.version to be installed automatically", tool.Name)
	}
	goos, goarch, err := platform()
	if err != nil {
		return spec{}, err
	}
	switch tool.Name {
	case "terraform":
		rawVersion := strings.TrimPrefix(version, "v")
		return spec{
			URL:        fmt.Sprintf("https://releases.hashicorp.com/terraform/%s/terraform_%s_%s_%s.zip", rawVersion, rawVersion, goos, goarch),
			Format:     "zip",
			BinaryPath: executableName("terraform"),
		}, nil
	case "helm":
		v := ensureV(version)
		return spec{
			URL:        fmt.Sprintf("https://get.helm.sh/helm-%s-%s-%s.tar.gz", v, goos, goarch),
			Format:     "tar.gz",
			BinaryPath: filepath.ToSlash(filepath.Join(goos+"-"+goarch, executableName("helm"))),
		}, nil
	case "kubectl":
		v := ensureV(version)
		return spec{
			URL:        fmt.Sprintf("https://dl.k8s.io/release/%s/bin/%s/%s/%s", v, goos, goarch, executableName("kubectl")),
			Format:     "binary",
			BinaryPath: executableName("kubectl"),
		}, nil
	case "kind":
		v := ensureV(version)
		return spec{
			URL:        fmt.Sprintf("https://github.com/kubernetes-sigs/kind/releases/download/%s/kind-%s-%s", v, goos, goarch),
			Format:     "binary",
			BinaryPath: executableName("kind"),
		}, nil
	default:
		return spec{}, fmt.Errorf("%s is not supported by managed toolchain", tool.Name)
	}
}

func canInstall(name string) bool {
	switch name {
	case "terraform", "helm", "kubectl", "kind":
		return true
	default:
		return false
	}
}

func desiredVersion(tool templateengine.Tool) string {
	if strings.TrimSpace(tool.Version) != "" {
		return strings.TrimSpace(tool.Version)
	}
	return strings.TrimSpace(tool.MinVersion)
}

func defaultVersionArgs(tool templateengine.Tool) []string {
	if len(tool.VersionArgs) > 0 {
		return tool.VersionArgs
	}
	return []string{"--version"}
}

func platform() (string, string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	switch goos {
	case "darwin", "linux":
	default:
		return "", "", fmt.Errorf("managed toolchain supports darwin and linux, got %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
	default:
		return "", "", fmt.Errorf("managed toolchain supports amd64 and arm64, got %s", goarch)
	}
	return goos, goarch, nil
}

func ensureV(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func download(url, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

func extractZipBinary(archivePath, binaryPath, target string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		if filepath.Base(file.Name) != filepath.Base(binaryPath) {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return err
		}
		defer src.Close()
		return writeExecutable(src, target)
	}
	return fmt.Errorf("binary %s not found in %s", binaryPath, archivePath)
}

func extractTarGzBinary(archivePath, binaryPath, target string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.ToSlash(header.Name) == binaryPath || filepath.Base(header.Name) == filepath.Base(binaryPath) {
			return writeExecutable(tr, target)
		}
	}
	return fmt.Errorf("binary %s not found in %s", binaryPath, archivePath)
}

func installBinary(path, target string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	return writeExecutable(src, target)
}

func writeExecutable(src io.Reader, target string) error {
	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}
