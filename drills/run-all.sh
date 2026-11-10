#!/usr/bin/env bash
# Runs every drill and prints a summary. Exit code is non-zero if any fail.
set -uo pipefail
cd "$(dirname "$0")"

# DRILLS overrides the set, e.g. the GitOps cluster runs D1 + D5.
read -ra drills <<< "${DRILLS:-d1-self-heal.sh d2-bad-release.sh d4-operator-down.sh}"
declare -A result
failed=0

for d in "${drills[@]}"; do
  echo "=================== $d ==================="
  start=$SECONDS
  if bash "$d"; then result[$d]="PASS ($(( SECONDS - start ))s)"; else result[$d]="FAIL"; failed=1; fi
done

echo
echo "================ Drill report ================"
for d in "${drills[@]}"; do printf '  %-24s %s\n' "$d" "${result[$d]}"; done
exit $failed
