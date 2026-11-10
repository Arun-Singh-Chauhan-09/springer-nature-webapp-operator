#!/usr/bin/env bash
# D1 Self-heal: someone deletes the Deployment. The operator must recreate it
# and the WebApp must return to Ready without human action.
source "$(dirname "$0")/lib.sh"

info "D1 self-heal: deleting deployment/$APP"
require_ready
k -n "$NS" delete deployment "$APP" --wait=false

wait_for 30 "Deployment recreated by operator" deployment_exists || fail "deployment was not recreated within 30s"
wait_for 120 "WebApp back to Ready" is_phase Ready || fail "WebApp did not return to Ready within 120s"
pass "D1 self-heal: operator restored the Deployment"
