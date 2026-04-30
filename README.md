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

`platformctl init` creates `platform.yaml`, validates the selected template, installs supported pinned tools into `~/.platformctl/bin`, and runs a non-blocking preflight for required tools and credentials. `platformctl preflight` (alias: `doctor`) checks dependencies, credentials, local write access, Docker readiness when required, and Kubernetes context when required. `platformctl plan` renders files under `generated/`, prints the steps that would be run, and writes `.platformctl/state.json`. `platformctl apply` asks for confirmation unless `--yes` is provided. If a workflow fails after some steps completed, fix the problem and run:

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

- Registry reference: `platformctl/aws-eks-standard` with `version: v1.0.0`
- Inline registry version: `platformctl/local-kind-standard@v1.0.0`
- Direct URL: `https://example.com/platform.template.yaml`
- Local directory: `./examples/local-templates/custom-minimal`

Example `platform.yaml`:

```yaml
template:
  source: platformctl/aws-eks-standard
  version: v1.0.0

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

The current manifest contract is `platformctl.io/v1alpha2`:

```yaml
apiVersion: platformctl.io/v1alpha2
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
- Steps have stable IDs, timeouts, optional retries, optional preflight metadata, and failure suggestions.
- Logs mask common `password=`, `secret=`, `token=`, and `key=` assignments.
- Local state is stored in `.platformctl/state.json`.
- State writes are atomic and `apply`/`destroy` create `.platformctl/lock` to prevent concurrent workflows.
- `apply`/`destroy` warn when the current template checksum or generated files differ from the last recorded plan.
- `apply` and `destroy` require interactive confirmation unless `--yes` is passed.

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

`platformctl init` and `platformctl plan` install missing supported tools into `~/.platformctl/bin`. `platformctl apply` and `platformctl destroy` use that directory before the global PATH. Supported managed tools are Terraform, Helm, kubectl, and Kind. Tools such as Docker and AWS CLI are checked but not installed automatically.

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

## More Documentation

- Template authoring guide: [docs/template-authoring.md](docs/template-authoring.md)
- Troubleshooting guide: [docs/troubleshooting.md](docs/troubleshooting.md)
