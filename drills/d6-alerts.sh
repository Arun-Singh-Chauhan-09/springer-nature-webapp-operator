#!/usr/bin/env bash
# D6 Alerting: prove the platform alerts reach Alertmanager when things break.
#   Part A: a bad release  -> WebAppNotReady fires, resolves after rollback
#   Part B: operator down  -> OperatorDown fires, resolves after restart
# In GitOps mode ArgoCD would undo these breakages within seconds (that is D5),
# so auto-sync is paused for the duration, as you would during a real incident
# drill, and always restored on exit.
# Run against the GitOps cluster: make drill-d6 (takes ~5 minutes)
source "$(dirname "$0")/lib.sh"

AM="/api/v1/namespaces/monitoring/services/kps-alertmanager:9093/proxy/api/v2/alerts"
APPS=(platform-root webapp-operator webapp-workloads)

alert_active() {   # alert_active <alertname>: true if Alertmanager has it active
  k get --raw "$AM" 2>/dev/null | jq -e --arg a "$1" \
    'any(.[]; .labels.alertname == $a and .status.state == "active")' >/dev/null
}
alert_gone() { ! alert_active "$1"; }

autosync() {       # autosync on|off
  local policy='null'
  [[ $1 == on ]] && policy='{"prune":true,"selfHeal":true}'
  for a in "${APPS[@]}"; do
    k -n argocd patch application "$a" --type merge -p "{\"spec\":{\"syncPolicy\":{\"automated\":$policy}}}" >/dev/null
  done
}

restore() {
  k -n "$NS" patch webapp "$APP" --type merge -p "{\"spec\":{\"image\":\"$GOOD_IMAGE\"}}" >/dev/null 2>&1 || true
  k -n "$OP_NS" scale deployment/webapp-operator --replicas=1 >/dev/null 2>&1 || true
  autosync on
  info "Auto-sync restored on: ${APPS[*]}"
}

require_ready
GOOD_IMAGE=$(k -n "$NS" get webapp "$APP" -o jsonpath='{.spec.image}')
trap restore EXIT

info "D6 alerting: pausing ArgoCD auto-sync on ${APPS[*]}"
autosync off

info "Part A: bad release ($GOOD_IMAGE -> 0.0.0-does-not-exist)"
k -n "$NS" patch webapp "$APP" --type merge -p "{\"spec\":{\"image\":\"${GOOD_IMAGE%:*}:0.0.0-does-not-exist\"}}" >/dev/null
wait_for 60 "WebApp reported Degraded" is_phase Degraded || fail "WebApp did not go Degraded"
wait_for 180 "Alertmanager received WebAppNotReady" alert_active WebAppNotReady \
  || fail "WebAppNotReady did not reach Alertmanager within 180s"
info "Rolling back to $GOOD_IMAGE"
k -n "$NS" patch webapp "$APP" --type merge -p "{\"spec\":{\"image\":\"$GOOD_IMAGE\"}}" >/dev/null
wait_for 120 "WebApp back to Ready" is_phase Ready || fail "WebApp did not recover"
wait_for 240 "WebAppNotReady resolved" alert_gone WebAppNotReady || fail "WebAppNotReady did not resolve"

info "Part B: operator down (scaling to 0)"
k -n "$OP_NS" scale deployment/webapp-operator --replicas=0 >/dev/null
wait_for 240 "Alertmanager received OperatorDown" alert_active OperatorDown \
  || fail "OperatorDown did not reach Alertmanager within 240s"
info "Bringing the operator back"
k -n "$OP_NS" scale deployment/webapp-operator --replicas=1 >/dev/null
k -n "$OP_NS" rollout status deployment/webapp-operator --timeout=120s >/dev/null
wait_for 240 "OperatorDown resolved" alert_gone OperatorDown || fail "OperatorDown did not resolve"

pass "D6 alerting: both platform alerts fired and resolved"
