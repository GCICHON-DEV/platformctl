# platformctl

`platformctl` is a simple bootstrapper for creating a real AWS developer platform.

It is not a replacement for Terraform, Helm, kubectl, or AWS CLI. It uses those tools underneath so a developer can create, inspect, use, and destroy a working platform without manually learning every DevOps step first.

The platform includes:

- AWS VPC
- AWS EKS
- managed node group
- kubeconfig
- namespaces for `dev`, `staging`, and `production`
- ArgoCD
- kube-prometheus-stack
- Prometheus
- Grafana
- ingress-nginx
- resource quotas, limit ranges, and baseline network policies

This MVP does not deploy application workloads yet. The next layer can add app commands for Docker image builds, chart generation, ECR push, and ArgoCD based deployment.

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

Install these tools locally:

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

The selected template provides the rest of the platform defaults: VPC CIDR, availability zone count, node group limits, `dev/staging/production` namespaces, ArgoCD, monitoring, ingress, quotas, and limit ranges.

Edit `platform.yaml` if you want to change the project name, AWS region, AWS profile, cluster name, Kubernetes version, node size, or node count.

The example uses EKS Kubernetes `1.34`, which is in standard support at the time this MVP was written.

## Templates

Templates are external YAML manifests loaded from a registry-style source.

```yaml
template:
  source: platformctl/aws-eks-standard
  version: v1.0.0
```

This is intentionally closer to Terraform modules or Helm chart repositories: the CLI does not hardcode the platform template in the binary. It resolves the registry reference to a remote template manifest, downloads it, caches it under `~/.platformctl/templates`, merges template defaults with user values, and then generates the internal Terraform/Kubernetes/Helm files.

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

The repo includes [examples/template-registry/aws-eks-standard.yaml](examples/template-registry/aws-eks-standard.yaml) as the first template manifest to publish into a separate template repository.

A template manifest contains non-secret defaults such as VPC CIDR, availability zone count, node group limits, `dev/staging/production` namespaces, ArgoCD, monitoring, ingress, quotas, and limit ranges.

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
2. Checks for `terraform`, `aws`, `kubectl`, and `helm`.
3. Generates internal files into `generated/`.
4. Runs `terraform init`.
5. Runs `terraform plan`.
6. Runs `terraform apply -auto-approve`.
7. Runs `aws eks update-kubeconfig`.
8. Applies Kubernetes namespaces, quotas, limit ranges, and network policies.
9. Installs ArgoCD, kube-prometheus-stack, and ingress-nginx.

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

`down` regenerates the internal Terraform files from `platform.yaml` and runs:

```bash
terraform destroy -auto-approve
```

It does not remove Helm charts first. Terraform deletion of the cluster and surrounding AWS resources is the source of truth for this MVP.

## What Happens Under The Hood

- Terraform creates AWS VPC, subnets, NAT Gateway, EKS, and managed node groups.
- AWS CLI updates your local kubeconfig.
- kubectl applies namespaces and baseline Kubernetes policies.
- Helm installs ArgoCD, Prometheus/Grafana, and ingress-nginx.

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
