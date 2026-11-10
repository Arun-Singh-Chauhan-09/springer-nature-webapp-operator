#!/usr/bin/env bash
# Shared helpers for failure drills.
set -euo pipefail

KCTX="${KCTX:-kind-webapp-demo}"
NS="${NS:-demo}"
APP="${APP:-podinfo}"
OP_NS="webapp-system"

k() { kubectl --context "$KCTX" "$@"; }

info() { printf '\033[36m[%s]\033[0m %s\n' "$(date +%H:%M:%S)" "$*"; }
pass() { printf '\033[32mPASS\033[0m %s\n' "$*"; exit 0; }
fail() { printf '\033[31mFAIL\033[0m %s\n' "$*"; exit 1; }

phase() { k -n "$NS" get webapp "$APP" -o jsonpath='{.status.phase}' 2>/dev/null || true; }

# wait_for <timeout-seconds> <description> <command...>
wait_for() {
  local timeout=$1 desc=$2; shift 2
  local start=$SECONDS
  until "$@" >/dev/null 2>&1; do
    (( SECONDS - start >= timeout )) && return 1
    sleep 2
  done
  info "$desc after $(( SECONDS - start ))s"
}

is_phase() { [[ "$(phase)" == "$1" ]]; }
deployment_exists() { k -n "$NS" get deployment "$APP"; }

require_ready() {
  wait_for 120 "WebApp is Ready" is_phase Ready || fail "precondition: $APP is not Ready (run 'make up' first)"
}
