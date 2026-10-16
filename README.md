# platformctl

`platformctl` is a platform bootstrap engine for creating developer platforms quickly from declarative blueprints.

It is not a CI/CD system and it is not a replacement for Terraform, Helm, kubectl, AWS CLI, Kind, or other platform tools. It resolves a platform template, validates inputs, renders declared files, shows an execution plan, and runs controlled platform workflows with local state and resumable checkpoints.

## Core Workflow

```bash
platformctl init --template platformctl/local-kind-standard --project demo
platformctl preflight
platformctl plan
platformctl apply
platformctl status
platformctl destroy
```

Backward-compatible aliases are still available:

```bash
platformctl up      # alias for apply
platformctl down    # alias for destroy
```

`platformctl init` creates `platform.yaml`, validates the selected template, installs supported pinned tools into `~/.platformctl/bin`, and runs a non-blocking preflight for required tools and credentials. `platformctl preflight` (alias: `doctor`) checks dependencies, credentials, local write access, Docker readiness when required, and Kubernetes context when required. `platformctl plan` renders files under `generated/`, prints a CLI workflow preview and its digest, and writes `.platformctl/state.json`. It does not install tools, execute `steps.plan`, run Terraform, or produce a saved Terraform plan. `platformctl apply` asks for confirmation unless `--yes` is provided. If a workflow fails after some steps completed, fix the problem and run:

```bash
platformctl apply --resume
```

Useful global flags:

```bash
platformctl plan --json
platformctl apply --yes --json
platformctl status --json
platformctl apply --verbose
platformctl plan --quiet
```

`--json` is intended for automation. Destructive commands require `--yes` with `--json` so prompts do not corrupt machine-readable output. `--verbose` includes the wrapped technical cause for failures.

## Templates

Templates can come from:

- Registry reference: `platformctl/aws-eks-standard` with `version: v1.1.0`
- Inline registry version: `platformctl/local-kind-standard@v1.1.0`
- Direct HTTPS URL: `https://example.com/platform.template.yaml`
- Local directory: `./examples/local-templates/custom-minimal`

Example `platform.yaml`:

```yaml
template:
  source: platformctl/aws-eks-standard
  version: v1.1.0

values:
  project_name: demo
  region: eu-central-1
  aws_profile: default
  cluster_name: demo-platform
```

Local template example:

```yaml
template:
  source: ./examples/local-templates/custom-minimal

values:
  project_name: demo
```

A local template directory contains `platform.template.yaml` and may include `files/`, `partials/`, or `docs/`. Template-rendered output is restricted to `generated/`.

## Template Manifest

The current manifest contract is `platformctl.io/v1beta1`:

```yaml
apiVersion: platformctl.io/v1beta1
kind: PlatformTemplate
metadata:
  name: custom-minimal
  description: Minimal custom platform blueprint.
  tags: [custom]
  maintainer: platformctl

inputs:
  project_name:
    description: Project identifier used in generated files.
    type: string
    required: true
    default: demo
    example: demo

requirements:
  credentials:
    - name: aws
      description: AWS profile must be valid.
      command: aws
      args: ["sts", "get-caller-identity", "--profile", "{{ .Values.aws_profile }}"]
  tools:
    - name: echo

files:
  - path: generated/custom/README.md
    source: files/README.md.tmpl

steps:
  plan:
    - name: show generated files
      id: show-generated-files
      command: echo
      args: ["generated files are ready"]
      timeout_seconds: 30
      suggestion: Verify generated/ is writable.
  apply:
    - name: apply platform
      id: apply-platform
      command: echo
      args: ["platform {{ .Values.project_name }} is ready"]
      timeout_seconds: 30
      suggestion: Inspect generated output and retry with platformctl apply --resume.
  destroy:
    - name: destroy platform
      id: destroy-platform
      command: echo
      args: ["platform {{ .Values.project_name }} removed"]
      timeout_seconds: 30
      suggestion: This template has no external resources to clean up.
```

Legacy `workflow.up` and `workflow.down` templates still load as compatibility aliases for `steps.apply` and `steps.destroy`.

## Built-in Blueprint Examples

The repo includes registry-style examples under `examples/template-registry`:

- `aws-eks-standard.yaml`: Terraform EKS, kubeconfig, ArgoCD, monitoring, and ingress.
- `local-kind-standard.yaml`: Kind cluster, ArgoCD, monitoring, and ingress.
- `azure-aks-standard.yaml`: AKS reference blueprint, outside the first supported AWS+local target.

The repo also includes a local custom template under `examples/local-templates/custom-minimal`.

## Safety Model

- Supported pinned tools are installed under `~/.platformctl/bin` and preferred over global PATH.
- Workflow steps can run only in the workspace root or under `generated/`.
- Workflow steps use a single executable plus explicit args; shell expressions are rejected.
- Credential checks use structured `command` + `args` and are never executed via shell.
- Steps have stable IDs, timeouts, optional retries, optional preflight metadata, and failure suggestions.
- Logs mask common `password=`, `secret=`, `token=`, and `key=` assignments.
- Local state is stored in `.platformctl/state.json`.
- `init`, `plan`, `apply`/`up`, and `destroy`/`down` hold one workspace lock before loading configuration through the final state checkpoint. A competing command fails before changing configuration, generated files, or state.
- State writes use a unique temporary file and atomic rename. A read-only `status` may observe the previous complete checkpoint while a workflow runs.
- On Linux and macOS, `.platformctl/lock` is a persistent file protected by a nonblocking kernel `flock`; file existence and its PID metadata do not indicate an active workflow. Never unlink or replace it while a CLI or workflow step may be running. Workflow steps inherit descriptor 3, retaining the lock after a CLI crash until their last inherited copy closes. Steps must preserve this descriptor, remain synchronous, and not explicitly unlock it; daemons or tools that close inherited descriptors need their own lifecycle coordination.
- A crash releases ownership once all inherited copies close. PID reuse cannot take over or strand a v2 kernel lock. Older PID-only locks are reclaimed only when their recorded PID is absent; a live/reused PID or malformed legacy lock fails closed. Upgrade and rollback require an idle workspace; never mix old and new binaries concurrently. See [lock recovery and rollback](docs/troubleshooting.md#workspace-lock-recovery-and-rollback).
- Generated files are reconciled from tracked `managed_files`; `platformctl` no longer deletes the whole `generated/` directory.
- The v2 preview digest covers rendered file bytes, source-file bytes, template identity/checksum, all input values, rendered steps/args, requirements and local target selectors. Raw input values and target configuration are not added to preview JSON/state; their digests are included. Existing rendered step arguments can still contain sensitive values.
- Target selectors include the workspace, `AWS_*`, `KUBE*`, `TF_*`, Google credential/config paths, Docker selectors/config, kubeconfig bytes (default, environment and `--kubeconfig` paths), Terraform workspace selection files (including `-chdir` and `TF_DATA_DIR`), and AWS config/credentials bytes. This binds local configuration, not live cloud account identity or remote infrastructure state. Custom script targets must be declared as template inputs; undeclared external script files/configuration are outside this contract.
- `apply`/`destroy` reject a changed recorded preview or edited/missing managed output before installing tools or writing files. Run `platformctl plan` to review and record a new preview, then approve without `--resume`.
- `apply --resume` and `destroy --resume` require a matching approval for that phase, not just a prior preview. Unchanged checkpoints are retained. Configuration is checked again after confirmation, after dependency setup and before each step; captured file bytes and step arguments are used for execution.
- `apply` and `destroy` require interactive confirmation unless `--yes` is passed. Cancellation, input EOF and `--json` without `--yes` preserve generated files/state and do not initialize/install the managed toolchain. The workspace lock file may be created or its diagnostic metadata updated.
- A workflow that changes its own bound target configuration (for example Kind updating the default kubeconfig) stops before its next step. Use an explicit isolated target and review changes before continuing; do not silently adopt a new context.
- Preview approval authorizes the CLI workflow only. It is not approval of a saved Terraform plan: `steps.plan` are shown, not executed, and provider changes are not evaluated by this command. Saved Terraform plan/apply support remains tracked in PLAT-31.

### Preview state upgrade and rollback

Older state has no v2 approval evidence. Execution with a recorded legacy hash and all legacy resume attempts fail closed. Preserve a private copy of `.platformctl/state.json` and managed output before running `platformctl plan`; this records a v2 preview and clears old checkpoints/approval without running infrastructure steps. Then approve a fresh workflow. If the old run has an uncertain result, inspect its resources first; re-planning is not reconciliation.

Rollback requires an idle workspace and the previous CLI binary. Preserve the current state and template pin. Do not resume a v2 approval with an older binary: it cannot enforce this contract. Restore an archived state only through a separately scoped recovery operation. Changing the binary does not undo infrastructure or data changes.

## Managed Toolchain

Templates can pin supported tool versions:

```yaml
requirements:
  tools:
    - name: terraform
      version: "1.5.7"
    - name: helm
      version: "v3.18.6"
    - name: kubectl
      version: "v1.34.0"
```

`platformctl init` installs missing supported tools into `~/.platformctl/bin`. `platformctl apply` and `platformctl destroy` verify/install tools only after approval and use that directory before the global PATH. `platformctl plan` does not install tools. Supported managed tools are Terraform, Helm, kubectl, and Kind. Tools such as Docker and AWS CLI are checked but not installed automatically.
Downloaded managed tools are verified with SHA256 checksums before installation.

## AWS Notes

The AWS EKS template can create billable resources, including EKS, NAT Gateway, EC2 worker nodes, EBS volumes, and LoadBalancer services.

Configure credentials before running `apply`:

```bash
aws configure --profile default
aws sts get-caller-identity --profile default
```

## Build And Test

```bash
go mod tidy
go test ./...
go test -race ./...
go build -o platformctl .
```

## Release Checklist

- CI green on main and PRs (`go vet ./...`, `go test ./...`, `go test -race ./...`).
- Manifest examples and docs use `platformctl.io/v1beta1`.
- Credential checks in templates use structured `command` + `args` (no shell command strings).
- Resume flows validated (`apply --resume` / `destroy --resume`) with unchanged plan hash.
- Managed toolchain install path validated with checksum verification.

## More Documentation

- Template authoring guide: [docs/template-authoring.md](docs/template-authoring.md)
- Troubleshooting guide: [docs/troubleshooting.md](docs/troubleshooting.md)

## Development and completion plan

- [Contribution and delivery flow](CONTRIBUTING.md)
- [Project audit and portfolio/engineering backlog](docs/project-audit.md)
- [Machine-readable planning backlog](docs/backlog.json)

The audit distinguishes static validation from real Kind/AWS acceptance. Cloud provisioning is not part of the planning/setup change.
