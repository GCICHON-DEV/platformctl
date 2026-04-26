package executor

type Terraform struct {
	runner *Runner
	dir    string
}

func NewTerraform(runner *Runner, dir string) *Terraform {
	return &Terraform{runner: runner, dir: dir}
}

func (t *Terraform) Init() error {
	return t.runner.RunStep("terraform init", t.dir, "terraform", "init")
}

func (t *Terraform) Plan() error {
	return t.runner.RunStep("terraform plan", t.dir, "terraform", "plan")
}

func (t *Terraform) Apply() error {
	return t.runner.RunStep("terraform apply", t.dir, "terraform", "apply", "-auto-approve")
}

func (t *Terraform) Destroy() error {
	return t.runner.RunStep("terraform destroy", t.dir, "terraform", "destroy", "-auto-approve")
}
