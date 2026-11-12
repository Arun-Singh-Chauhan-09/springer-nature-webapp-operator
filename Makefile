SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c

CLUSTER ?= webapp-demo
IMG     ?= webapp-operator:dev
GO_IMG  ?= golang:1.24
KCTX    := kind-$(CLUSTER)
KUBECTL ?= kubectl --context $(KCTX)

.DEFAULT_GOAL := help

##@ General
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage: make <target>\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Develop (Go runs inside Docker; no local Go install needed)
tidy: ## Resolve Go dependencies and write go.sum
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMG) go mod tidy

test: ## Run go vet + unit tests
	docker run --rm -v "$(CURDIR)":/src -w /src $(GO_IMG) sh -c "go mod tidy && go vet ./... && go test ./... -count=1"

build: ## Build the operator image
	docker build -t $(IMG) .

##@ Platform (kind)
cluster: ## Create the kind cluster if it does not exist
	@kind get clusters | grep -qx $(CLUSTER) || kind create cluster --config hack/kind-config.yaml --wait 90s

deploy: build ## Build, load and deploy the operator + CRD
	kind load docker-image $(IMG) --name $(CLUSTER)
	$(KUBECTL) apply -f config/crd/
	$(KUBECTL) wait --for=condition=Established crd/webapps.platform.example.com --timeout=60s
	$(KUBECTL) apply -k config/operator/
	$(KUBECTL) -n webapp-system rollout restart deployment/webapp-operator
	$(KUBECTL) -n webapp-system rollout status deployment/webapp-operator --timeout=120s

sample: ## Deploy the sample WebApp and wait until Ready
	$(KUBECTL) apply -f apps/podinfo.yaml
	$(KUBECTL) -n demo wait webapp/podinfo --for=condition=Ready --timeout=180s
	$(KUBECTL) -n demo get webapp

up: cluster deploy sample ## Everything: cluster + operator + sample app
	@echo; echo "Platform is up. Try: kubectl -n demo get webapp,deploy,svc,pods"

status: ## Show WebApps and their children
	$(KUBECTL) get webapp -A
	$(KUBECTL) -n demo get deploy,svc,pods -l app.kubernetes.io/managed-by=webapp-operator

logs: ## Tail operator logs
	$(KUBECTL) -n webapp-system logs deploy/webapp-operator -f

metrics: ## Print the operator's webapp_* metrics
	$(KUBECTL) -n webapp-system port-forward svc/webapp-operator-metrics 18080:8080 >/dev/null 2>&1 & \
	PF=$$!; sleep 2; curl -s localhost:18080/metrics | grep -E '^(webapp_|controller_runtime_reconcile_total)'; kill $$PF

##@ GitOps (Terraform + ArgoCD)
GITOPS_CLUSTER ?= webapp-gitops
GITOPS_KCTX    := kind-$(GITOPS_CLUSTER)
GK             := kubectl --context $(GITOPS_KCTX)
TF_DIR         := infra/envs/kind
TF             ?= terraform
TF_IMG         ?= hashicorp/terraform:1.9
PIN_SHA        ?= $(shell git rev-parse --short=7 HEAD)

gitops-up: ## Terraform creates kind + ArgoCD; ArgoCD deploys CRD -> operator -> apps from Git
	$(TF) -chdir=$(TF_DIR) init -input=false
	$(TF) -chdir=$(TF_DIR) apply -input=false -auto-approve -var cluster_name=$(GITOPS_CLUSTER)
	@echo "Waiting for ArgoCD to sync waves: monitoring/loki -> alloy/crds -> operator -> workloads ..."
	@for i in $$(seq 1 180); do $(GK) -n argocd get application webapp-workloads >/dev/null 2>&1 && break; sleep 5; done
	$(GK) -n argocd wait application/webapp-workloads --for=jsonpath='{.status.health.status}'=Healthy --timeout=900s
	$(GK) -n demo wait webapp/podinfo --for=condition=Ready --timeout=300s
	@$(MAKE) --no-print-directory gitops-status

gitops-status: ## Show ArgoCD Applications (sync + health) and the WebApps
	$(GK) -n argocd get applications.argoproj.io
	$(GK) get webapp -A

argocd-ui: ## Print the admin password and port-forward the ArgoCD UI to localhost:8080
	@echo "ArgoCD UI: http://localhost:8080  user: admin  password: $$($(GK) -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)"
	$(GK) -n argocd port-forward svc/argocd-server 8080:80

gitops-drills: ## D1 self-heal + D5 GitOps drift + D6 alerting on the GitOps cluster (~6 min)
	KCTX=$(GITOPS_KCTX) DRILLS="d1-self-heal.sh d5-gitops-drift.sh d6-alerts.sh" bash ./drills/run-all.sh

grafana-ui: ## Grafana on http://localhost:3000 (admin/admin), dashboard "Platform Health"
	@echo "Grafana: http://localhost:3000  user: admin  password: admin  -> Dashboards -> Platform Health"
	$(GK) -n monitoring port-forward svc/grafana 3000:80

alerts: ## List alerts currently active in Alertmanager
	@$(GK) get --raw /api/v1/namespaces/monitoring/services/kps-alertmanager:9093/proxy/api/v2/alerts \
	  | jq -r '.[] | [.labels.alertname, .labels.severity, .status.state, (.annotations.summary // "")] | @tsv' | column -t -s $$'\t'

pin-image: ## Pin the GitOps operator image to a CI-built commit (PIN_SHA=<7-char sha>, default HEAD)
	sed -i 's/^\(\s*newTag:\).*/\1 sha-$(PIN_SHA)/' gitops/operator/kustomization.yaml
	@grep newTag gitops/operator/kustomization.yaml

tf-check: ## terraform fmt + validate (in a container, no local install needed)
	docker run --rm --user $$(id -u):$$(id -g) -e TF_DATA_DIR=/tmp/tf -e HOME=/tmp -v "$(CURDIR)":/w -w /w --entrypoint sh $(TF_IMG) -c \
	  "terraform fmt -check -recursive infra && cd $(TF_DIR) && terraform init -backend=false -input=false >/dev/null && terraform validate"

gitops-down: ## Destroy the GitOps cluster with Terraform
	$(TF) -chdir=$(TF_DIR) destroy -input=false -auto-approve -var cluster_name=$(GITOPS_CLUSTER)

##@ Drills
drill-d1: ## D1 self-heal: delete the Deployment
	KCTX=$(KCTX) bash ./drills/d1-self-heal.sh
drill-d2: ## D2 bad release: push a broken image, then roll back
	KCTX=$(KCTX) bash ./drills/d2-bad-release.sh
drill-d4: ## D4 operator down: prove the operator is what heals
	KCTX=$(KCTX) bash ./drills/d4-operator-down.sh
drill-d5: ## D5 GitOps drift (GitOps cluster only)
	KCTX=$(GITOPS_KCTX) bash ./drills/d5-gitops-drift.sh
drill-d6: ## D6 alerting: WebAppNotReady + OperatorDown fire and resolve (GitOps cluster only, ~5 min)
	KCTX=$(GITOPS_KCTX) bash ./drills/d6-alerts.sh
drill-all: ## Run all drills and print a PASS/FAIL report
	KCTX=$(KCTX) bash ./drills/run-all.sh

##@ Cleanup
down: ## Delete the kind cluster
	kind delete cluster --name $(CLUSTER)

.PHONY: help tidy test build cluster deploy sample up status logs metrics gitops-up gitops-status argocd-ui gitops-drills grafana-ui alerts pin-image tf-check gitops-down drill-d1 drill-d2 drill-d4 drill-d5 drill-d6 drill-all down
