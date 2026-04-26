package bootstrap

import (
	"fmt"
	"path/filepath"

	"platformctl/internal/config"
	"platformctl/internal/executor"
)

type Bootstrap struct {
	runner       *executor.Runner
	generatedDir string
	cfg          *config.Config
	helm         *executor.Helm
	kubectl      *executor.Kubectl
}

func New(runner *executor.Runner, generatedDir string, cfg *config.Config) *Bootstrap {
	return &Bootstrap{
		runner:       runner,
		generatedDir: generatedDir,
		cfg:          cfg,
		helm:         executor.NewHelm(runner),
		kubectl:      executor.NewKubectl(runner),
	}
}

func (b *Bootstrap) Run() error {
	if err := b.addHelmRepos(); err != nil {
		return err
	}
	if err := b.ApplyNamespaces(); err != nil {
		return err
	}
	if err := b.InstallArgoCD(); err != nil {
		return err
	}
	if err := b.InstallMonitoring(); err != nil {
		return err
	}
	if err := b.InstallIngress(); err != nil {
		return err
	}
	return nil
}

func (b *Bootstrap) addHelmRepos() error {
	repos := []struct {
		name string
		url  string
	}{
		{"argo", "https://argoproj.github.io/argo-helm"},
		{"prometheus-community", "https://prometheus-community.github.io/helm-charts"},
		{"ingress-nginx", "https://kubernetes.github.io/ingress-nginx"},
	}

	for _, repo := range repos {
		if err := b.helm.RepoAdd(repo.name, repo.url); err != nil {
			return err
		}
	}
	return b.helm.RepoUpdate()
}

func (b *Bootstrap) valuesFile(name string) string {
	return filepath.Join(b.generatedDir, "helm-values", name)
}

func (b *Bootstrap) manifestsDir() string {
	return filepath.Join(b.generatedDir, "kubernetes")
}

func (b *Bootstrap) printSkipped(component string) {
	fmt.Fprintf(b.runner.Stdout, "\n==> skipping %s because it is disabled\n", component)
}
