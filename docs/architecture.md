# Target architecture

Phase 1 implements steps 4, 6, 7, 8, 10 (D1, D2, D4) and the CI part of step 2. See [ROADMAP.md](../ROADMAP.md).

```
┌─────────────────────────────────────────────────────────────────┐
│ 0. TERRAFORM                                                     │
│   infra/envs/kind  ──► kind cluster                              │
│   infra/envs/eks   ──► VPC + EKS + nodes + EBS CSI               │
│   modules/platform-bootstrap ──► ArgoCD + root app-of-apps       │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 1. GIT REPO  (single source of truth)                            │
│   cmd/ api/ internal/   operator code                            │
│   config/               CRD + operator manifests                 │
│   apps/                 developer WebApps                        │
│   drills/               failure scenarios                        │
└─────────────────────────────────────────────────────────────────┘
                              │ push / PR
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 2. GITHUB ACTIONS                                                │
│   gofmt · vet · unit tests · kubeconform · kind e2e + drills     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 3. ARGOCD  (sync waves: CRDs → operator + monitoring → apps)     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 4. DEVELOPER writes a 6-line WebApp                              │
│    image · replicas · port · healthPath · needsDatabase          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 5. ADMISSION                                                     │
│   now:   CRD schema + CEL (pinned tag, no :latest, port >= 1024) │
│   later: webhook with thesis security checks                     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 6. KUBERNETES API SERVER  (stores the WebApp)                    │
└─────────────────────────────────────────────────────────────────┘
                              │ watch / reconcile
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 7. WEBAPP OPERATOR  (Go, controller-runtime)                     │
│   • CreateOrUpdate Deployment + Service (ownerReferences)        │
│   • platform defaults: limits, probes, non-root, seccomp, labels │
│   • status: phase + Ready condition with reason                  │
│     (Progressing / Ready / Degraded: ImagePullBackOff, ...)      │
│   • metrics: webapp_ready, webapp_phase, webapp_replicas         │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 8. CLUSTER (kind / EKS)   Deployment · Service · (Postgres)      │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 9. OBSERVABILITY  Prometheus · Loki · Grafana · Alertmanager     │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 10. DRILLS  D1 self-heal · D2 bad release · D3 DB restore        │
│             D4 operator down · D5 drift                          │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ 11. FEEDBACK LOOP → ROADMAP.md                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Design decisions

- **Edit children in place, don't replace them.** `mutateDeployment` sets only the fields the platform owns, and sets every probe field explicitly. Fields the API server defaults are left alone, so `CreateOrUpdate` writes only on real change. A unit test asserts the Deployment's resourceVersion is unchanged after repeated reconciles.
- **Selectors are set once.** They are immutable, so they're set only at creation and kept a strict subset of the labels.
- **Degraded is a first-class state.** A stuck rollout is not just "not ready yet": the operator inspects pods for reasons that won't fix themselves (ImagePullBackOff, CrashLoopBackOff, …) and reports them in the Ready condition, so developers see *why*.
- **Requeue while not ready.** Pod-level failures don't change the Deployment the controller watches, so a not-ready WebApp is re-checked every 10s.
- **Validation as early as possible.** Cheap rules (pinned image, port range) live in the CRD as OpenAPI/CEL, so bad specs are rejected by the API server before they reach the operator.
