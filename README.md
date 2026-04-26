# platformctl

`platformctl` is a simple template-driven bootstrapper for creating developer platforms.

It is not a replacement for Terraform, Helm, kubectl, AWS CLI, Azure CLI, Kind, or other platform tools. It is a small engine that downloads a platform template, renders the files declared by that template, and runs the workflow declared by that template.

Templates can target different environments:

- `platformctl/aws-eks-standard`
- `platformctl/azure-aks-standard`
- `platformctl/local-kind-standard`

This MVP does not deploy application workloads yet. The next layer can add app commands for Docker image builds, chart generation, registry push, and ArgoCD based deployment.

## Commands

The public CLI is intentionally small:

```bash
platformctl check
platformctl validate
platformctl up
platformctl down
```

`platformctl` always reads `platform.yaml` from the current directory. The file points at a remote template manifest and provides only the values that should be different from the template defaults.

## Requirements

Install the tools required by the selected template. For the AWS template:

- Terraform
- AWS CLI
- kubectl
- Helm

Go is needed only when building `platformctl` from source.

Check your machine:

```bash
./platformctl check
```

Also check Go:

```bash
./platformctl check --dev
```

On macOS with Homebrew, install missing runtime dependencies:

```bash
./platformctl check --install
```

The AWS resources created by this project can generate costs, especially EKS, NAT Gateway, EC2 worker nodes, EBS volumes, and LoadBalancer services.

## AWS Credentials

Configure AWS credentials before running `up`.

```bash
aws configure --profile default
aws sts get-caller-identity --profile default
```

The profile and region are read from `platform.yaml` values:

```yaml
template:
  source: platformctl/aws-eks-standard
  version: v1.0.0

values:
  aws_region: eu-central-1
  aws_profile: default
```

## Configure

Start from the example config:

```bash
cp examples/platform.yaml platform.yaml
```

The default `platform.yaml` is intentionally short:

```yaml
template:
  source: platformctl/aws-eks-standard
  version: v1.0.0

values:
  project_name: demo
  aws_region: eu-central-1
  aws_profile: default
  cluster_name: demo-platform
```

The selected template provides the rest of the platform defaults and workflow.

Edit `platform.yaml` if you want to change the project name, AWS region, AWS profile, cluster name, Kubernetes version, node size, or node count.

The example uses EKS Kubernetes `1.34`, which is in standard support at the time this MVP was written.

## Templates

Templates are external YAML manifests loaded from a registry-style source.

```yaml
template:
  source: platformctl/aws-eks-standard
  version: v1.0.0
```

This is intentionally closer to Terraform modules or Helm chart repositories: the CLI does not hardcode provider-specific platform logic. It resolves the registry reference to a remote template manifest, downloads it, caches it under `~/.platformctl/templates`, merges template defaults with user values, renders files declared by the template, and runs the workflow declared by the template.

The built-in registry namespace currently resolves like this:

```text
platformctl/aws-eks-standard@v1.0.0
  -> https://raw.githubusercontent.com/GCICHON-DEV/platformctl-templates/v1.0.0/aws-eks-standard.yaml
```

For local testing or private hosting, override the registry base URL:

```bash
export PLATFORMCTL_TEMPLATE_REGISTRY_URL=http://localhost:8765
```

Full HTTP/HTTPS URLs are also supported in `template.source` for direct template manifests.

The repo includes example template manifests under [examples/template-registry](examples/template-registry):

- `aws-eks-standard.yaml`
- `azure-aks-standard.yaml`
- `local-kind-standard.yaml`

A template manifest can define:

- required input values
- default values
- required local tools
- files to render into `generated/`
- the `up` workflow
- the `down` workflow
- success messages

Minimal shape:

```yaml
apiVersion: platformctl.io/v1
kind: PlatformTemplate
name: my-template

requirements:
  tools:
    - name: terraform
    - name: kubectl

inputs:
  project_name:
    required: true

defaults:
  cluster_name: demo

files:
  - path: generated/example.txt
    content: |
      project={{ .Values.project_name }}

workflow:
  up:
    - name: example
      command: echo
      args: ["hello", "{{ .Values.project_name }}"]
  down:
    - name: cleanup
      command: echo
      args: ["bye"]
```

All template-rendered files must be written under `generated/`.

## Secrets

Do not put secrets in `platform.yaml`, templates, or generated files.

Current platform secrets are handled outside the config:

- AWS credentials come from AWS CLI profiles such as `~/.aws/credentials`.
- ArgoCD and Grafana generate Kubernetes `Secret` objects during installation.
- Application secrets are not implemented yet.

The intended future direction for app secrets is AWS Secrets Manager plus External Secrets Operator. In that model, Git and ArgoCD contain only references to secrets, not secret values.

## Build

```bash
go mod tidy
go build -o platformctl .
```

## Validate

```bash
./platformctl validate
```

This checks `platform.yaml` and reports configuration problems before any AWS resources are created.

## Create The Platform

```bash
./platformctl check
./platformctl validate
./platformctl up
```

`up` performs these steps:

1. Loads and validates `platform.yaml`.
2. Checks the tools required by the selected template.
3. Generates files declared by the selected template into `generated/`.
4. Runs the selected template's `workflow.up` steps.

## Verify

```bash
kubectl get nodes
kubectl get ns
kubectl get pods -A
```

## ArgoCD Access

```bash
kubectl -n argocd port-forward svc/argocd-server 8080:443
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d; echo
```

Open:

```text
https://localhost:8080
```

The default username is `admin`.

## Grafana Access

```bash
kubectl -n monitoring port-forward svc/monitoring-grafana 3000:80
kubectl -n monitoring get secret monitoring-grafana -o jsonpath='{.data.admin-password}' | base64 -d; echo
```

Open:

```text
http://localhost:3000
```

The default username comes from the selected template or `values.grafana_admin_user`.

## Destroy

```bash
./platformctl down
```

`down` regenerates files from the selected template and runs the selected template's `workflow.down` steps.

## What Happens Under The Hood

- AWS templates can use Terraform modules, AWS CLI, kubectl, and Helm.
- Azure templates can use Terraform AzureRM, Azure CLI, kubectl, and Helm.
- Local templates can use Docker, Kind, kubectl, and Helm.

You do not need to run these steps manually for the MVP workflow.

## Roadmap

Future app-level commands can build on this platform:

```bash
platformctl app init
platformctl app build
platformctl app deploy --env dev
```

The intended direction is Docker image build, ECR push, Helm chart or values generation, ArgoCD `Application` generation, and GitOps based deployment to the platform created by `platformctl up`.

## Notes

- Terraform backend support is intentionally limited to local state for the MVP.
- `cert-manager`, `external-dns`, and AWS Load Balancer Controller are represented as configuration placeholders. Setting them to `true` is rejected until those addons are implemented.
- No secrets are written by the CLI.
