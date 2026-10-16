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

Local state is stored in `.platformctl/state.json`. All mutating commands (`init`, `plan`, `apply`/`up`, `destroy`/`down`) hold the same lock throughout configuration loading, rendering, confirmation, execution, and checkpoint writes. A rejected command leaves these files and checkpoints untouched. `status` reads the last complete checkpoint and does not acquire the workflow lock.

If a workflow fails after some steps complete:

```bash
platformctl apply --resume
platformctl destroy --resume
```

`apply` and `destroy` reject changed recorded previews or edited/missing managed files before tool installation or mutation. The v2 digest binds rendered/source bytes, inputs, steps and local target selectors. Resume also requires a matching approval for that phase. Preserve state and inspect any partially completed resources before running `platformctl plan` again, then approve `apply` or `destroy` without `--resume`; re-planning clears checkpoints and is not resource reconciliation.

`platformctl plan` is a CLI preview: it renders files and records a digest, but does not install tools, execute `steps.plan`, or run `terraform plan`. Approval is not evidence of a saved Terraform plan. Legacy state has no v2 approval evidence and requires explicit re-planning. See [preview state upgrade and rollback](../README.md#preview-state-upgrade-and-rollback).

Changing kubeconfig, AWS config/profile, Terraform environment selectors or declared target inputs invalidates approval. This includes changes made by a running step: execution stops before the next step and preserves completed checkpoints. Inspect the target and review the new preview instead of editing approval hashes or bypassing the check.


## Workspace Lock Recovery And Rollback

Linux and macOS use a nonblocking kernel `flock` on the persistent `.platformctl/lock` file. Its `platformctl-lock-v2` metadata records the last owner for diagnosis; the kernel lock decides whether the workspace is busy, regardless of PID reuse. The CLI never deletes this file, including during release or signal handling. Do not remove/replace the file or its parent directory while any CLI or workflow step may be active, and use a local filesystem supporting `flock` (no cross-machine coordination guarantee).

Steps inherit the lock as descriptor 3. After SIGINT/SIGTERM/SIGKILL of only the CLI, a surviving synchronous step retains ownership until it exits and closes its inherited copies. Wait for that step, or inspect and stop it deliberately before retrying. Templates must preserve descriptor 3 and must not unlock it or daemonize; tools that close inherited descriptors are outside this guarantee. Full cancellation/process-group handling and reconciliation of an uncertain external result belong to PLAT-21. A completed external action without a saved checkpoint may run again on resume; inspect its result before resuming.

For an older PID-only lock, an absent PID allows automatic in-place conversion. A live PID (including reuse by another process), malformed legacy contents, or an unsupported filesystem causes a safe refusal. There is no age-based expiry. First confirm the workspace has no CLI or descendant step running; only then preserve a diagnostic copy and remove a legacy/corrupt lock for manual recovery. Never delete `state.json`, Terraform state/locks, generated data, or kubeconfig to recover a workflow lock.

The `state.json` schema and template contract are unchanged. Upgrade only after all old workflows and descendants finish. Old binaries do not understand v2 locks: for rollback, stop new workflows and descendants, preserve the v2 diagnostic lock outside the workspace, remove only the idle lock file, and restore the previous CLI binary with the same template pin and state. Do not run old and new versions concurrently. Binary rollback does not reverse infrastructure or restore data.
