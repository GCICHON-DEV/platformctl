package templateengine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type targetSnapshot struct {
	Workspace          string
	Environment, Files map[string]string
}

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
	runtimeEnvironment := plan.Target.Environment()
	for key, value := range runtimeEnvironment {
		environment[key] = value
	}
	envValue := func(key string) string {
		if value, ok := runtimeEnvironment[key]; ok {
			return value
		}
		return os.Getenv(key)
	}
	kubeconfigs := filepath.SplitList(envValue("KUBECONFIG"))
	if len(kubeconfigs) == 0 {
		kubeconfigs = []string{filepath.Join(home, ".kube", "config")}
	}
	paths := append([]string{}, kubeconfigs...)
	for _, item := range []struct{ key, fallback string }{
		{"AWS_CONFIG_FILE", filepath.Join(home, ".aws", "config")},
		{"AWS_SHARED_CREDENTIALS_FILE", filepath.Join(home, ".aws", "credentials")},
	} {
		path := envValue(item.key)
		if path == "" {
			path = item.fallback
		}
		paths = append(paths, path)
	}
	dockerConfig := envValue("DOCKER_CONFIG")
	if dockerConfig == "" {
		dockerConfig = filepath.Join(home, ".docker")
	}
	paths = append(paths, filepath.Join(dockerConfig, "config.json"))
	if path := envValue("AWS_CA_BUNDLE"); path != "" {
		paths = append(paths, path)
	}
	// Tools resolve relative environment paths from their own working
	// directory, which can differ from the CLI workspace.
	directories := []string{"."}
	for _, steps := range [][]PlannedStep{plan.PlanSteps, plan.ApplySteps, plan.DestroySteps} {
		for _, step := range steps {
			directory, err := stepWorkingDirectory(step)
			if err != nil {
				return "", err
			}
			directories = append(directories, step.Dir, directory)
		}
	}
	inheritedPaths := paths
	paths = nil
	for _, path := range inheritedPaths {
		if path == "" {
			continue
		}
		if filepath.IsAbs(path) {
			paths = append(paths, path)
			continue
		}
		for _, directory := range directories {
			paths = append(paths, filepath.Join(directory, path))
		}
	}
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
				directory, err := stepWorkingDirectory(step)
				if err != nil {
					return "", err
				}
				dataDir := os.Getenv("TF_DATA_DIR")
				if dataDir == "" {
					dataDir = ".terraform"
				}
				if !filepath.IsAbs(dataDir) {
					dataDir = filepath.Join(directory, dataDir)
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
		if plan.Target != nil {
			path = absolute
		}
		// A declared target config is bound to the captured bytes, even before
		// the first render. GeneratedHash verifies its actual bytes at execution.
		if digest, ok := generated[absolute]; ok {
			files[path] = digest
			continue
		}
		var data []byte
		if plan.Target != nil {
			data, err = ReadTargetFile(path, 1024*1024)
		} else {
			data, err = os.ReadFile(path)
		}
		if errors.Is(err, os.ErrNotExist) {
			files[path] = "missing"
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read target configuration %s: %w", path, err)
		}
		files[path] = checksum(data)
	}
	plan.targetSnapshot = &targetSnapshot{workspace, environment, files}
	return digestJSON(plan.targetSnapshot)
}

// Terraform applies its global -chdir before resolving per-directory data.
func stepWorkingDirectory(step PlannedStep) (string, error) {
	directory := step.Dir
	if filepath.Base(step.Command) != "terraform" {
		return directory, nil
	}
	found := false
	for index := 0; index < len(step.Args); index++ {
		arg := step.Args[index]
		path := ""
		switch {
		case strings.HasPrefix(arg, "-chdir="):
			path = strings.TrimPrefix(arg, "-chdir=")
		case arg == "-chdir":
			index++
			if index >= len(step.Args) {
				return "", fmt.Errorf("terraform -chdir requires a directory")
			}
			path = step.Args[index]
		default:
			continue
		}
		if found {
			return "", fmt.Errorf("multiple terraform -chdir options are not supported")
		}
		found = true
		if path != "" {
			if filepath.IsAbs(path) {
				directory = path
			} else {
				directory = filepath.Join(step.Dir, path)
			}
		}
	}
	return directory, nil
}
