# webapp-operator

[![ci](https://github.com/Arun-Singh-Chauhan-09/springer-nature-webapp-operator/actions/workflows/ci.yaml/badge.svg)](https://github.com/Arun-Singh-Chauhan-09/springer-nature-webapp-operator/actions/workflows/ci.yaml)

> Personal portfolio project built for a Springer Nature Platform Engineer application. Not affiliated with or endorsed by Springer Nature.


A small internal-developer-platform built around a Kubernetes operator in Go.
Product teams write a six-line `WebApp`; the platform turns it into a secure,
observable Deployment and Service, reports status back, and heals itself.

> Built as a platform-engineering showcase for the Springer Nature
> Platform Engineer role (Engineering Enablement): platform as a product,
> self-service, Kubernetes operators, observability and reliability drills.

```yaml
apiVersion: platform.example.com/v1
kind: WebApp
metadata:
  name: podinfo
  namespace: demo
spec:
  image: ghcr.io/stefanprodan/podinfo:6.7.1
  replicas: 2
  port: 9898
  healthPath: /healthz
```

```
$ kubectl -n demo get webapp
NAME      IMAGE                                DESIRED   READY   PHASE   AGE
podinfo   ghcr.io/stefanprodan/podinfo:6.7.1   2         2       Ready   1m
```

## What the platform adds for free

| The developer writes | The platform adds |
|---|---|
| `image` | Rejects `:latest` and untagged images at the API server (CEL) |
| `port` | Named container port + ClusterIP Service |
| `healthPath` | Readiness and liveness probes |
| (nothing) | CPU/memory requests and limits, non-root, seccomp `RuntimeDefault`, no privilege escalation, all capabilities dropped, no SA token mount |
| (nothing) | `app.kubernetes.io/*` labels for observability |
| (nothing) | Status: `Progressing` → `Ready`, or `Degraded` with the reason (e.g. `ImagePullBackOff`) |
| `needsDatabase` | Reported as a condition today; CloudNativePG provisioning on the roadmap |

## Quick start

Requirements: Docker, [kind](https://kind.sigs.k8s.io/), kubectl, make, bash.
On Windows, run everything from **WSL2** (Ubuntu) with Docker Desktop's WSL integration on.
Go is **not** required locally: it runs inside Docker.

```bash
make up          # kind cluster + build/load operator + CRD + sample app, waits until Ready
make status      # WebApps and their children
make drill-all   # run failure drills, prints a PASS/FAIL report
make test        # go vet + unit tests (in a golang container)
make down        # delete the cluster
```

Try the guardrails:

```bash
kubectl apply -f apps/examples/rejected-latest-tag.yaml   # rejected: ':latest' not allowed
kubectl apply -f apps/examples/with-database.yaml         # DatabaseReady=Unknown condition
kubectl -n demo describe webapp podinfo                   # conditions + events
make metrics                                              # webapp_ready, webapp_phase, ...
```

## Failure drills

| Drill | What happens | Expected |
|---|---|---|
| D1 self-heal | `kubectl delete deployment podinfo` | Operator recreates it; WebApp back to Ready |
| D2 bad release | Image changed to a tag that does not exist | WebApp `Degraded` with reason `ImagePullBackOff`, old pods keep serving; rollback → Ready |
| D4 operator down | Operator scaled to 0, Deployment deleted | Nothing heals while it's down (proves the operator is the healer); recovers on restart |

D3 (database restore) and D5 (drift → ArgoCD revert) come with later phases. All drills also run in CI on a kind cluster.

## Repository layout

```
api/v1/                 WebApp types (CRD Go API)
internal/controller/    reconciler, platform defaults, metrics, unit tests
cmd/                    manager entrypoint
config/crd/             CRD with OpenAPI + CEL validation and printer columns
config/operator/        namespace, RBAC (least privilege), operator Deployment, metrics Service
apps/                   developer-side WebApps
drills/                 failure drill scripts
hack/                   kind config
.github/workflows/      CI: gofmt, vet, tests, kubeconform, kind e2e + drills
docs/architecture.md    target architecture and design decisions
ROADMAP.md              phases and feedback backlog
```

## Roadmap

Phase 1 (this) is the operator core. Next: Terraform + ArgoCD GitOps, the Grafana/Loki/Prometheus/Alertmanager stack, CloudNativePG + an admission webhook based on my MEng thesis on the security of Kubernetes manifests, and EKS. See [ROADMAP.md](ROADMAP.md) and [docs/architecture.md](docs/architecture.md).

## Author

Arun Singh Chauhan, DevOps / Cloud / Platform Engineer, Berlin
