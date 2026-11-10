#!/usr/bin/env bash
# D4 Operator down: "who watches the watcher?"
# With the operator stopped, a deleted Deployment stays deleted (proving the
# operator, not Kubernetes, does the healing). When the operator comes back it
# must catch up on its own. Alerting on this (OperatorDown) arrives with the
# observability phase.
source "$(dirname "$0")/lib.sh"

restore() { k -n "$OP_NS" scale deployment webapp-operator --replicas=1 >/dev/null; }
trap restore EXIT

require_ready
info "D4 operator down: scaling operator to 0"
k -n "$OP_NS" scale deployment webapp-operator --replicas=0
k -n "$OP_NS" wait --for=delete pod -l app.kubernetes.io/name=webapp-operator --timeout=60s || true

k -n "$NS" delete deployment "$APP" --wait=true
info "Waiting 20s with the operator down..."
sleep 20
deployment_exists >/dev/null 2>&1 && fail "deployment came back without the operator; something else is reconciling it"
info "Confirmed: nothing heals the app while the operator is down"

info "Bringing operator back"
restore
trap - EXIT
wait_for 60 "Operator caught up and recreated the Deployment" deployment_exists || fail "operator did not recreate the deployment"
wait_for 120 "WebApp back to Ready" is_phase Ready || fail "WebApp did not return to Ready"
pass "D4 operator down: outage contained, operator recovered on restart"
