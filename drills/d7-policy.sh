#!/usr/bin/env bash
# D7 Workload policy: the admission webhook rejects unsafe manifests at `kubectl apply`.
#   1. An unsafe Deployment (the kind my thesis found LLMs generate) must be DENIED,
#      naming the Trivy checks it violates.
#   2. A hardened Deployment must be ADMITTED with no warnings.
#   3. A safe-but-sloppy Deployment (no limits, writable root fs) must be ADMITTED
#      WITH WARNINGS: hygiene gaps are reported, not blocked.
# Uses --dry-run=server, so the webhook runs but nothing is created.
# Run against the GitOps cluster: make drill-d7
source "$(dirname "$0")/lib.sh"

apply_dry() { k apply --dry-run=server -f - 2>&1; }

UNSAFE=$(cat <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata: {name: policy-drill-unsafe, namespace: demo}
spec:
  selector: {matchLabels: {app: policy-drill}}
  template:
    metadata: {labels: {app: policy-drill}}
    spec:
      hostNetwork: true
      volumes: [{name: host, hostPath: {path: /}}]
      containers:
        - name: app
          image: nginx:latest
          securityContext: {privileged: true}
          volumeMounts: [{name: host, mountPath: /host}]
YAML
)

HARDENED=$(cat <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata: {name: policy-drill-hardened, namespace: demo}
spec:
  selector: {matchLabels: {app: policy-drill}}
  template:
    metadata: {labels: {app: policy-drill}}
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: app
          image: ghcr.io/stefanprodan/podinfo:6.7.1
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          resources:
            limits: {cpu: 250m, memory: 128Mi}
YAML
)

# Hardened minus limits and read-only root filesystem.
SLOPPY=$(sed -e 's/policy-drill-hardened/policy-drill-sloppy/' \
             -e '/readOnlyRootFilesystem/d' -e '/resources:/d' -e '/limits:/d' <<<"$HARDENED")

info "D7 policy: 1/3 unsafe Deployment (hostNetwork, hostPath /, privileged, nginx:latest)"
if out=$(apply_dry <<<"$UNSAFE"); then
  fail "unsafe Deployment was ADMITTED: $out"
fi
for id in KSV017 KSV009 KSV023 KSV013 KSV012 KSV001; do
  grep -q "$id" <<<"$out" || fail "denial does not mention $id: $out"
done
info "Denied as expected:"
grep -o 'denied by platform policy.*' <<<"$out" | tr ';' '\n' | sed 's/^/    /' | head -8

info "D7 policy: 2/3 hardened Deployment"
out=$(apply_dry <<<"$HARDENED") || fail "hardened Deployment was DENIED: $out"
grep -q '^Warning' <<<"$out" && fail "hardened Deployment should have no warnings: $out"
info "Admitted, no warnings"

info "D7 policy: 3/3 safe but sloppy Deployment (no limits, writable root fs)"
out=$(apply_dry <<<"$SLOPPY") || fail "sloppy Deployment was DENIED (should only warn): $out"
for id in KSV011 KSV018 KSV014; do
  grep -q "Warning: \[$id\]" <<<"$out" || fail "expected a $id warning: $out"
done
info "Admitted with warnings:"
grep '^Warning' <<<"$out" | sed 's/^/    /'

pass "D7 policy: unsafe workload denied at the API, hardened admitted, hygiene gaps surfaced as warnings"
