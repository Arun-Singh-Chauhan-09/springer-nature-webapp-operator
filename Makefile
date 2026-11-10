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
	$(KUBECTL) apply -f config/operator/
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

##@ Drills
drill-d1: ## D1 self-heal: delete the Deployment
	KCTX=$(KCTX) bash ./drills/d1-self-heal.sh
drill-d2: ## D2 bad release: push a broken image, then roll back
	KCTX=$(KCTX) bash ./drills/d2-bad-release.sh
drill-d4: ## D4 operator down: prove the operator is what heals
	KCTX=$(KCTX) bash ./drills/d4-operator-down.sh
drill-all: ## Run all drills and print a PASS/FAIL report
	KCTX=$(KCTX) bash ./drills/run-all.sh

##@ Cleanup
down: ## Delete the kind cluster
	kind delete cluster --name $(CLUSTER)

.PHONY: help tidy test build cluster deploy sample up status logs metrics drill-d1 drill-d2 drill-d4 drill-all down
