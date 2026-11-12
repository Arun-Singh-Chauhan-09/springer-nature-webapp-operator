# Roadmap

Built in phases. Every phase ends in a working, demoable state.

| Phase | Scope | Status |
|---|---|---|
| 1 | Go operator: `WebApp` CRD with CEL validation, Deployment + Service children, platform defaults (limits, probes, non-root, labels), status conditions, Prometheus metrics, unit tests, kind setup, drills D1/D2/D4, CI with kind e2e | ✅ done |
| 2 | Terraform (`infra/envs/kind`, shared `modules/platform-bootstrap`) + ArgoCD app-of-apps with sync waves; drill D5 (drift → ArgoCD revert) | ✅ done (kind) |
| 3 | Observability: kube-prometheus-stack, Loki, Grafana "Platform Health" dashboard, Alertmanager rules (`WebAppNotReady`, `OperatorDown`, `ReconcileErrorsHigh`); alert drill D6 asserts they reach Alertmanager | ✅ done (kind) |
| 4a | Validating admission webhook enforcing the Trivy KSV checks from my MEng thesis (deny/warn, opt-in per namespace), cert-manager TLS, metrics, drill D7 | ✅ done |
| 4b | `needsDatabase` → CloudNativePG Postgres cluster + finalizer; drill D3 (DB restore) | planned |
| 5 | `infra/envs/eks` (VPC + EKS v21 + EBS CSI via Pod Identity + gp3 default StorageClass), reusing `platform-bootstrap`; validated in CI with validate/tflint/checkov. Not applied, to avoid ~$260/month; cost, apply and teardown steps in `infra/envs/eks/README.md` | ✅ code done, not applied |

## Feedback loop (platform as a product)

Ideas collected from "users" of the platform go here and get prioritised like product features:

- Show the failing pod's message directly in `kubectl get webapp` (wide output).
- Allow env vars and secrets references in the WebApp spec.
- Optional Ingress/HTTPRoute exposure.
