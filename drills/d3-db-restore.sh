#!/usr/bin/env bash
# D3 Database restore: an untested backup is not a backup.
#   1. Write a unique marker row into the orders app's database.
#   2. Take a backup through the Barman Cloud plugin (to the platform object store).
#   3. Restore that backup into a NEW cluster and check the marker row is there.
# The live database is never touched by the restore; the restored copy is deleted at the end.
# Run against the GitOps cluster: make drill-d3 (~5 minutes)
APP="${APP:-orders}"
source "$(dirname "$0")/lib.sh"

DB="${APP}-db"
TS=$(date +%s)
MARKER="d3-$TS"
BACKUP="${DB}-d3-$TS"
RESTORE="${DB}-restore"

psql_on() {   # psql_on <cluster> <sql>: run SQL on that cluster's primary, database "app"
  local primary
  primary=$(k -n "$NS" get cluster "$1" -o jsonpath='{.status.currentPrimary}')
  k -n "$NS" exec "$primary" -c postgres -- psql -d app -v ON_ERROR_STOP=1 -tAc "$2"
}
backup_done() { [[ "$(k -n "$NS" get backup "$BACKUP" -o jsonpath='{.status.phase}')" == completed ]]; }
backup_failed() { [[ "$(k -n "$NS" get backup "$BACKUP" -o jsonpath='{.status.phase}')" == failed ]]; }
restore_ready() { k -n "$NS" get cluster "$RESTORE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' | grep -q True; }
db_ready() { k -n "$NS" get webapp "$APP" -o jsonpath='{.status.conditions[?(@.type=="DatabaseReady")].status}' | grep -q True; }

cleanup() {
  k -n "$NS" delete cluster "$RESTORE" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  k -n "$NS" delete backup "$BACKUP" --ignore-not-found >/dev/null 2>&1 || true
}
trap cleanup EXIT
k -n "$NS" delete cluster "$RESTORE" --ignore-not-found --wait=true >/dev/null 2>&1 || true

require_ready
wait_for 60 "Database is ready" db_ready || fail "precondition: $APP has no ready database"

info "D3 restore: 1/3 writing marker row '$MARKER' into $DB"
psql_on "$DB" "CREATE TABLE IF NOT EXISTS drill_marker (note text PRIMARY KEY, at timestamptz DEFAULT now());
               INSERT INTO drill_marker (note) VALUES ('$MARKER');" >/dev/null
[[ "$(psql_on "$DB" "SELECT count(*) FROM drill_marker WHERE note = '$MARKER'")" == 1 ]] || fail "marker row not written"

info "D3 restore: 2/3 backing up $DB to the platform object store"
k apply -f - >/dev/null <<YAML
apiVersion: postgresql.cnpg.io/v1
kind: Backup
metadata: {name: $BACKUP, namespace: $NS}
spec:
  cluster: {name: $DB}
  method: plugin
  pluginConfiguration: {name: barman-cloud.cloudnative-pg.io}
YAML
start=$SECONDS
until backup_done; do
  backup_failed && fail "backup failed: $(k -n "$NS" get backup "$BACKUP" -o jsonpath='{.status.error}')"
  (( SECONDS - start >= 300 )) && fail "backup not completed within 300s"
  sleep 3
done
info "Backup completed after $(( SECONDS - start ))s"

info "D3 restore: 3/3 restoring the backup into a new cluster $RESTORE"
k apply -f - >/dev/null <<YAML
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata: {name: $RESTORE, namespace: $NS}
spec:
  instances: 1
  storage: {size: 1Gi}
  resources:
    requests: {cpu: 100m, memory: 256Mi}
    limits: {cpu: 500m, memory: 512Mi}
  bootstrap:
    recovery: {source: origin}
  externalClusters:
    - name: origin
      plugin:
        name: barman-cloud.cloudnative-pg.io
        parameters: {barmanObjectName: platform-backups, serverName: $DB}
YAML
wait_for 600 "Restored cluster is Ready" restore_ready || fail "restored cluster not Ready within 600s"

got=$(psql_on "$RESTORE" "SELECT count(*) FROM drill_marker WHERE note = '$MARKER'")
[[ "$got" == 1 ]] || fail "marker row '$MARKER' missing from the restored database (count=$got)"
info "Marker row '$MARKER' found in the restored database"

pass "D3 restore: backup taken, restored into a new cluster, data verified"
