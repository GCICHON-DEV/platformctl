package templateengine

import (
	"os"
	"path/filepath"
)

// Environment binds config selectors before a child changes its working
// directory. It is shared by the snapshot, identity checks and actual runner.
// It never changes the parent process environment.
func (target *Target) Environment() map[string]string {
	if target == nil {
		return nil
	}
	abs := func(path string) string {
		value, err := filepath.Abs(path)
		if err != nil {
			return path
		}
		return value
	}
	home, _ := os.UserHomeDir()
	home = abs(home)
	values := map[string]string{"HOME": home}
	docker := os.Getenv("DOCKER_CONFIG")
	if docker == "" {
		docker = filepath.Join(home, ".docker")
	}
	values["DOCKER_CONFIG"] = abs(docker)
	if target.AWS != nil {
		values["AWS_PROFILE"], values["AWS_DEFAULT_PROFILE"] = target.AWS.Profile, target.AWS.Profile
		values["AWS_REGION"], values["AWS_DEFAULT_REGION"] = target.AWS.Region, target.AWS.Region
		for _, key := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_SECURITY_TOKEN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN", "AWS_ROLE_SESSION_NAME"} {
			values[key] = ""
		}
		for key, fallback := range map[string]string{"AWS_CONFIG_FILE": filepath.Join(home, ".aws", "config"), "AWS_SHARED_CREDENTIALS_FILE": filepath.Join(home, ".aws", "credentials")} {
			path := os.Getenv(key)
			if path == "" {
				path = fallback
			}
			values[key] = abs(path)
		}
		if path := os.Getenv("AWS_CA_BUNDLE"); path != "" {
			values["AWS_CA_BUNDLE"] = abs(path)
		}
	}
	if target.Kubernetes != nil {
		paths := []string{}
		if target.Kubernetes.Kubeconfig != "" {
			paths = []string{target.Kubernetes.Kubeconfig}
		} else {
			paths = filepath.SplitList(os.Getenv("KUBECONFIG"))
		}
		if len(paths) == 0 {
			paths = []string{filepath.Join(home, ".kube", "config")}
		}
		for i, path := range paths {
			if path != "" {
				paths[i] = abs(path)
			}
		}
		values["KUBECONFIG"] = joinPaths(paths)
	}
	return values
}

func joinPaths(paths []string) string {
	var value string
	for _, path := range paths {
		if path == "" {
			continue
		}
		if value != "" {
			value += string(os.PathListSeparator)
		}
		value += path
	}
	return value
}
