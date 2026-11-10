# Roadmap

Built in phases. Every phase ends in a working, demoable state.

| Phase | Scope | Status |
|---|---|---|
| 1 | Go operator: `WebApp` CRD with CEL validation, Deployment + Service children, platform defaults (limits, probes, non-root, labels), status conditions, Prometheus metrics, unit tests, kind setup, drills D1/D2/D4, CI with kind e2e | ✅ in this repo |
| 2 | Terraform (`infra/envs/kind`, shared `modules/platform-bootstrap`) + ArgoCD app-of-apps with sync waves; drill D5 (drift → ArgoCD revert) | planned |
| 3 | Observability: kube-prometheus-stack, Loki, Grafana "Platform Health" dashboard, Alertmanager rules (`WebAppNotReady`, `OperatorDown`, `ReconcileErrorsHigh`); D2/D4 extended to assert alerts fire | planned |
| 4 | `needsDatabase` → CloudNativePG Postgres cluster + finalizer; validating/mutating admission webhook with the security checks from my MEng thesis; drill D3 (DB restore) | planned |
| 5 | `infra/envs/eks` (VPC + EKS + EBS CSI) validated in CI with tflint/checkov; one real EKS run; recorded demo | planned |

## Feedback loop (platform as a product)

Ideas collected from "users" of the platform go here and get prioritised like product features:

- Show the failing pod's message directly in `kubectl get webapp` (wide output).
- Allow env vars and secrets references in the WebApp spec.
- Optional Ingress/HTTPRoute exposure.
