#!/usr/bin/env bash
# D2 Bad release: a developer ships an image tag that does not exist.
# The platform must surface it as Degraded (with the reason), and a rollback
# must bring it back to Ready. In the GitOps phase the rollback becomes a git revert.
source "$(dirname "$0")/lib.sh"

require_ready
GOOD_IMAGE=$(k -n "$NS" get webapp "$APP" -o jsonpath='{.spec.image}')
BAD_IMAGE="ghcr.io/stefanprodan/podinfo:0.0.0-does-not-exist"

restore() { k -n "$NS" patch webapp "$APP" --type merge -p "{\"spec\":{\"image\":\"$GOOD_IMAGE\"}}" >/dev/null; }
trap restore EXIT

info "D2 bad release: $GOOD_IMAGE -> $BAD_IMAGE"
k -n "$NS" patch webapp "$APP" --type merge -p "{\"spec\":{\"image\":\"$BAD_IMAGE\"}}"

wait_for 90 "WebApp reported Degraded" is_phase Degraded || fail "WebApp was not marked Degraded within 90s"
REASON=$(k -n "$NS" get webapp "$APP" -o jsonpath='{.status.conditions[?(@.type=="Ready")].reason}')
info "Reason surfaced to developer: $REASON"

info "Rolling back to $GOOD_IMAGE"
restore
trap - EXIT
wait_for 120 "WebApp back to Ready" is_phase Ready || fail "WebApp did not recover after rollback"
pass "D2 bad release: detected ($REASON) and recovered"
