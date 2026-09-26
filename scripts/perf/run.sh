#!/usr/bin/env bash
# Prepara un entorno de medicion: BD desechable "argos_perf", migrada y sembrada con hosts sinteticos, y un server de
# pruebas en el puerto 18082. NUNCA toca la BD real. Uso:
#   bash scripts/perf/run.sh setup [hosts=1000] [days=3]   crea la BD, migra y siembra
#   bash scripts/perf/run.sh start                          arranca el server (deja el pid en $PERF_DIR/server.pid)
#   bash scripts/perf/run.sh load [args de loadgen]         corre el generador de carga (ver loadgen/main.go)
#   bash scripts/perf/run.sh stop                           para el server
#   bash scripts/perf/run.sh drop                           borra la BD de medicion
# Requiere: psql/pg_dump de PostgreSQL 18 en PGBIN, y el superusuario de la BD local en PGUSER/PGPASSWORD.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PERF_DIR="${PERF_DIR:-${TMPDIR:-/tmp}/argos-perf}"
PGBIN="${PGBIN:-/c/Program Files/PostgreSQL/18/bin}"
export PGUSER="${PGUSER:-argos}" PGPASSWORD="${PGPASSWORD:-argos}" PGHOST="${PGHOST:-localhost}"
DB=argos_perf
PORT=18082
PERF_PASS="${PERF_PASS:-Perf-2026-carga-xyz}"
psql() { "$PGBIN/psql" -v ON_ERROR_STOP=1 "$@"; }
mkdir -p "$PERF_DIR"
bin="$PERF_DIR/argos-server"
[[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]] && bin="$bin.exe"

env_server() {
	export DATABASE_URL="postgres://$PGUSER:$PGPASSWORD@$PGHOST:5432/$DB?sslmode=disable"
	export ARGOS_LISTEN_ADDR="127.0.0.1:$PORT" ARGOS_PUBLIC_URL="http://127.0.0.1:$PORT" ARGOS_INSECURE_COOKIES=true
	export ARGOS_SECRET_KEY="$(head -c 32 /dev/zero | base64)" # solo pruebas
}

case "${1:-}" in
setup)
	hosts="${2:-1000}" days="${3:-3}"
	(cd "$root/server" && go build -o "$bin" ./cmd/server)
	psql -d postgres -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)" -c "CREATE DATABASE $DB"
	env_server
	timeout 60 "$bin" migrate >/dev/null
	psql -q -d "$DB" -v hosts="$hosts" -v days="$days" -f "$root/scripts/perf/seed.sql"
	printf '%s\n' "$PERF_PASS" | "$bin" create-admin perf >/dev/null
	echo "listo: $DB con $hosts hosts x $days dias; admin perf / \$PERF_PASS"
	;;
start)
	env_server
	"$bin" >"$PERF_DIR/server.log" 2>&1 &
	echo $! >"$PERF_DIR/server.pid"
	for _ in $(seq 1 30); do curl -sf "http://127.0.0.1:$PORT/health" >/dev/null && { echo "server en :$PORT"; exit 0; }; sleep 1; done
	echo "el server no arranco; log: $PERF_DIR/server.log" >&2
	exit 1
	;;
load)
	shift
	(cd "$root/scripts/perf/loadgen" && go run . -base "http://127.0.0.1:$PORT" -user perf -pass "$PERF_PASS" "$@")
	;;
stop)
	[[ -f "$PERF_DIR/server.pid" ]] && kill "$(cat "$PERF_DIR/server.pid")" 2>/dev/null || true
	rm -f "$PERF_DIR/server.pid"
	;;
drop)
	psql -d postgres -c "DROP DATABASE IF EXISTS $DB WITH (FORCE)"
	;;
*)
	sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
