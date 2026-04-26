package executor

type Kubectl struct {
	runner *Runner
}

func NewKubectl(runner *Runner) *Kubectl {
	return &Kubectl{runner: runner}
}

func (k *Kubectl) Apply(path string) error {
	return k.runner.RunStep("kubectl apply platform manifests", "", "kubectl", "apply", "-f", path)
}
