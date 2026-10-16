package templateengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const PlanFormat = "platformctl-preview-v2"

// snapshot binds the preview to rendered bytes, all input values (including
// unused ones), source bytes, execution steps and local target selectors.
// Sensitive input and target values are represented by digests only.
func (r *Resolved) snapshot(p *ExecutionPlan) error {
	p.Format = PlanFormat
	p.contents = map[string]string{}
	p.FileDigests = map[string]string{}
	p.SourceDigests = map[string]string{}
	for _, file := range r.Manifest.Files {
		path := filepath.Clean(file.Path)
		if _, exists := p.contents[path]; exists {
			return fmt.Errorf("duplicate generated path %s", path)
		}
		raw, err := r.generatedSource(file)
		if err != nil {
			return err
		}
		content, err := r.Render(raw)
		if err != nil {
			return fmt.Errorf("render %s: %w", path, err)
		}
		p.contents[path] = content
		p.FileDigests[path] = checksum([]byte(content))
		if file.Source != "" {
			p.SourceDigests[file.Source] = checksum([]byte(raw))
		}
	}
	var generated strings.Builder
	for _, path := range p.GeneratedFiles {
		generated.WriteString(path)
		generated.WriteByte(0)
		generated.WriteString(p.contents[path])
		generated.WriteByte(0)
	}
	p.GeneratedDigest = checksum([]byte(generated.String()))
	var err error
	p.InputsDigest, err = digestJSON(struct {
		Platform PlatformFile
		Values   map[string]interface{}
	}{r.Platform, r.Values})
	if err != nil {
		return err
	}
	p.TargetDigest, err = targetDigest(p)
	if err != nil {
		return err
	}
	p.Digest, err = digestJSON(p) // Digest is still empty; contents is intentionally private.
	return err
}

func digestJSON(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return checksum(data), nil
}

// This captures local selectors, not live cluster/cloud identity or remote
// infrastructure state. Custom scripts must declare their target in inputs.
func targetDigest(plan *ExecutionPlan) (string, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	environment := map[string]string{}
	for _, item := range os.Environ() {
		key, value, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "AWS_") || strings.HasPrefix(key, "KUBE") || strings.HasPrefix(key, "TF_") || key == "GOOGLE_APPLICATION_CREDENTIALS" || key == "CLOUDSDK_CONFIG" || strings.HasPrefix(key, "DOCKER_") {
			environment[key] = value
		}
	}
	kubeconfigs := filepath.SplitList(os.Getenv("KUBECONFIG"))
	if len(kubeconfigs) == 0 {
		kubeconfigs = []string{filepath.Join(home, ".kube", "config")}
	}
	paths := append([]string{}, kubeconfigs...)
	for _, item := range []struct{ key, fallback string }{
		{"AWS_CONFIG_FILE", filepath.Join(home, ".aws", "config")},
		{"AWS_SHARED_CREDENTIALS_FILE", filepath.Join(home, ".aws", "credentials")},
	} {
		path := os.Getenv(item.key)
		if path == "" {
			path = item.fallback
		}
		paths = append(paths, path)
	}
	dockerConfig := os.Getenv("DOCKER_CONFIG")
	if dockerConfig == "" {
		dockerConfig = filepath.Join(home, ".docker")
	}
	paths = append(paths, filepath.Join(dockerConfig, "config.json"))
	for _, steps := range [][]PlannedStep{plan.PlanSteps, plan.ApplySteps, plan.DestroySteps} {
		for _, step := range steps {
			for index, arg := range step.Args {
				path := ""
				if strings.HasPrefix(arg, "--kubeconfig=") {
					path = strings.TrimPrefix(arg, "--kubeconfig=")
				}
				if arg == "--kubeconfig" && index+1 < len(step.Args) {
					path = step.Args[index+1]
				}
				if path != "" {
					if !filepath.IsAbs(path) {
						path = filepath.Join(step.Dir, path)
					}
					paths = append(paths, path)
				}
			}
			if filepath.Base(step.Command) == "terraform" {
				dataDir := os.Getenv("TF_DATA_DIR")
				if dataDir == "" {
					dataDir = ".terraform"
				}
				if !filepath.IsAbs(dataDir) {
					dataDir = filepath.Join(step.Dir, dataDir)
				}
				paths = append(paths, filepath.Join(dataDir, "environment"))
			}
		}
	}
	generated := map[string]string{}
	for path, digest := range plan.FileDigests {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		generated[absolute] = digest
	}
	files := map[string]string{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		// A declared target config is bound to the captured bytes, even before
		// the first render. GeneratedHash verifies its actual bytes at execution.
		if digest, ok := generated[absolute]; ok {
			files[path] = digest
			continue
		}
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			files[path] = "missing"
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read target configuration %s: %w", path, err)
		}
		files[path] = checksum(data)
	}
	return digestJSON(struct {
		Workspace          string
		Environment, Files map[string]string
	}{workspace, environment, files})
}
