package executor

type AWS struct {
	runner *Runner
}

func NewAWS(runner *Runner) *AWS {
	return &AWS{runner: runner}
}

func (a *AWS) UpdateKubeconfig(region, clusterName, profile string) error {
	args := []string{"eks", "update-kubeconfig", "--region", region, "--name", clusterName}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	return a.runner.RunStep("aws eks update-kubeconfig", "", "aws", args...)
}
