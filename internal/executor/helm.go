package executor

type Helm struct {
	runner *Runner
}

func NewHelm(runner *Runner) *Helm {
	return &Helm{runner: runner}
}

func (h *Helm) RepoAdd(name, url string) error {
	return h.runner.RunStep("helm repo add "+name, "", "helm", "repo", "add", name, url, "--force-update")
}

func (h *Helm) RepoUpdate() error {
	return h.runner.RunStep("helm repo update", "", "helm", "repo", "update")
}

func (h *Helm) UpgradeInstall(release, chart, namespace, valuesFile string) error {
	args := []string{"upgrade", "--install", release, chart, "-n", namespace, "--create-namespace"}
	if valuesFile != "" {
		args = append(args, "-f", valuesFile)
	}
	return h.runner.RunStep("helm upgrade --install "+release, "", "helm", args...)
}
