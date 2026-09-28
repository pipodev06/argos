#!/usr/bin/env bash
# Mide la purga de un host con MUCHO historial: crea una BD desechable "argos_volume_test", genera las filas por SQL
# (generate_series, sin generar nada a mano), purga un host con la forma anterior (una sola transaccion) y otro con la
# actual (por tandas de un dia), imprime tiempo / WAL / transaccion mas larga de cada una y borra la BD. NUNCA toca
# la BD real. Uso: bash scripts/perf/purge-volume.sh [filas_por_host=6000000]
# Variables: ARGOS_VOLUME_SKIP_BASELINE=1 omite la medicion de una sola transaccion; ARGOS_VOLUME_RUN=<regex> elige el
# test (por defecto TestPurgeVolume y TestPurgeConcurrentVolume, este ultimo con 10 bajas simultaneas).
# Requiere: PostgreSQL 18 en PGBIN y el usuario local en PGUSER/PGPASSWORD.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PGBIN="${PGBIN:-/c/Program Files/PostgreSQL/18/bin}"
export PGUSER="${PGUSER:-argos}" PGPASSWORD="${PGPASSWORD:-argos}" PGHOST="${PGHOST:-localhost}"
DB=argos_volume_test
ROWS="${1:-6000000}"

psql() { "$PGBIN/psql" -v ON_ERROR_STOP=1 -q -d postgres "$@"; }
cleanup() { psql -c "DROP DATABASE IF EXISTS $DB" >/dev/null 2>&1 || true; }
trap cleanup EXIT

cleanup
psql -c "CREATE DATABASE $DB"
cd "$root/server"
ARGOS_TEST_DATABASE_URL="postgres://$PGUSER:$PGPASSWORD@$PGHOST:5432/$DB?sslmode=disable" ARGOS_VOLUME_ROWS="$ROWS" \
	go test ./internal/store -run "${ARGOS_VOLUME_RUN:-^TestPurge(Concurrent)?Volume$}" -count=1 -v -timeout 90m
