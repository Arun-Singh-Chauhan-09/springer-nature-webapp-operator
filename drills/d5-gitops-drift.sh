#!/usr/bin/env bash
# D5 GitOps drift: someone "fixes" production by hand (kubectl edit) instead of
# changing Git. ArgoCD's selfHeal must put the WebApp back to what Git says.
# Run against the GitOps cluster: make gitops-drills
source "$(dirname "$0")/lib.sh"

replicas() { k -n "$NS" get webapp "$APP" -o jsonpath='{.spec.replicas}'; }
replicas_is() { [[ "$(replicas)" == "$1" ]]; }

require_ready
desired=$(replicas)
info "D5 GitOps drift: Git says replicas=$desired, scaling to 5 by hand"
k -n "$NS" patch webapp "$APP" --type merge -p '{"spec":{"replicas":5}}' >/dev/null
info "Manual change applied, live replicas=$(replicas)"

wait_for 180 "ArgoCD reverted replicas to $desired (the Git value)" replicas_is "$desired" \
  || fail "ArgoCD did not revert the drift within 180s (is selfHeal enabled?)"
wait_for 120 "WebApp back to Ready" is_phase Ready || fail "WebApp did not return to Ready within 120s"
pass "D5 GitOps drift: manual change reverted, Git is the source of truth"
