# Troubleshooting

## Start With Preflight

Run:

```bash
platformctl preflight
platformctl preflight --strict
platformctl preflight --json
```

`preflight` checks template requirements, managed toolchain availability, credentials, and local readiness checks such as Docker and Kubernetes context when the template requires them. `doctor` is an alias for `preflight`.

## Read Errors

Errors use a stable code plus context and a fix when available:

```text
Error [PLATFORMCTL_STEP_FAILED]: workflow step apply:03:terraform-apply failed
Context: step=apply:03:terraform-apply name="terraform apply" dir="generated/terraform" command="terraform apply -auto-approve"
Fix: Check AWS credentials, quota, and Terraform provider errors. After fixing the issue, run platformctl apply --resume.
```

Use `--verbose` to include the wrapped technical cause.

## Common Problems

Missing dependency:

```bash
platformctl preflight
```

Install the missing tool or pin a managed version for Terraform, Helm, kubectl, or Kind in the template.

Credential failure:

```bash
aws sts get-caller-identity --profile default
az account show
```

Fix local credentials before running `apply`.

Docker or Kind failure:

```bash
docker info
kind get clusters
```

Start Docker Desktop or delete the conflicting cluster with `platformctl destroy`.

Kubernetes readiness failure:

```bash
kubectl config current-context
kubectl get nodes
kubectl get pods -A
```

Verify kubeconfig points at the intended cluster.

Terraform failure:

```bash
cd generated/terraform
terraform init
terraform plan
```

Check provider downloads, state locks, account quota, and cloud permissions.

## State And Resume

Local state is stored in `.platformctl/state.json`. `apply` and `destroy` create `.platformctl/lock` while running to prevent concurrent workflows. If a process is interrupted, the lock is removed on normal interrupt handling; if the process is force-killed, remove a stale lock only after confirming no `platformctl` process is running.

If a workflow fails after some steps complete:

```bash
platformctl apply --resume
platformctl destroy --resume
```

`platformctl` warns when generated files or the template checksum differ from the last recorded plan. In that case, run `platformctl plan` again before applying.
