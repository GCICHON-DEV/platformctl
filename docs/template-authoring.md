# Template Authoring

Templates define the files, requirements, workflow steps, and operator-facing output used by `platformctl`.

## Manifest Contract

Use `apiVersion: platformctl.io/v1alpha2` and `kind: PlatformTemplate`.

Required metadata:

```yaml
metadata:
  name: local-kind-standard
  description: Local Kind developer platform with ArgoCD, monitoring, and ingress.
  tags: [local, kind]
  maintainer: platformctl
```

Each input must include a description. Prefer examples and validation patterns for values that become cloud or Kubernetes resource names.

```yaml
inputs:
  cluster_name:
    description: Kubernetes cluster name.
    type: string
    required: true
    default: platformctl-local
    example: demo-local
    validation: "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$"
```

Supported input types are `string`, `integer`, `number`, `boolean`, `array`, and `object`.

## Files

Generated output must stay under `generated/`.

```yaml
files:
  - path: generated/kubernetes/environments.yaml
    content: |
      apiVersion: v1
      kind: Namespace
      metadata:
        name: dev
```

Local template directories may reference source files relative to the template directory:

```yaml
files:
  - path: generated/custom/README.md
    source: files/README.md.tmpl
```

## Steps

Steps use a single executable name plus explicit args. Do not use shell expressions.

```yaml
steps:
  apply:
    - id: helm-install-argocd
      name: install ArgoCD
      command: helm
      args: ["upgrade", "--install", "argocd", "argo/argo-cd", "--wait", "--timeout", "10m"]
      timeout_seconds: 900
      suggestion: Inspect ArgoCD pods and retry with platformctl apply --resume.
```

Step quality checklist:

- Set a stable `id` for every step.
- Set `timeout_seconds` for every network, cloud, Helm, Terraform, or Kubernetes operation.
- Add `suggestion` for every step that can fail due to credentials, quota, network, locks, or cluster readiness.
- Use `helm upgrade --install --wait --timeout ...` for Helm releases.
- Use `retry.attempts` only for transient operations; do not hide deterministic validation failures.

## Outputs

Use `outputs.success`, `outputs.notes`, and `outputs.next_steps` to tell the operator exactly what happened and what to run next.

```yaml
outputs:
  success:
    - "Check pods: kubectl get pods -A"
  notes:
    - "This template creates billable cloud resources."
  next_steps:
    - "Run platformctl status to inspect local state."
```
