#!/usr/bin/env bash
# Levanta el server en tu máquina de desarrollo (Windows/Git Bash o Linux/macOS), contra la BD real local
# "argos". Compila (server + web si falta/pides --web), lo deja corriendo en segundo plano.
#
# Uso:
#   bash scripts/run-local.sh start [--web]   compila y arranca (--web también recompila el frontend)
#   bash scripts/run-local.sh stop            detiene el server (si está corriendo)
#   bash scripts/run-local.sh status          dice si está corriendo y el resultado de /health
#   bash scripts/run-local.sh logs            sigue el log en vivo (Ctrl+C para salir, no lo detiene)
#
# En PowerShell (Windows), usa el alias `argos-local` (de $PROFILE) en vez de escribir el bash completo:
#   argos-local start | argos-local status | argos-local stop | argos-local logs
#
# Requiere: Postgres local con la BD "argos" ya creada (usuario/clave "argos", localhost:5432) y la clave de
# cifrado en ~/.argos-dev/secret-key.txt (créala una vez con `openssl rand -hex 32 > ~/.argos-dev/secret-key.txt`
# si no existe). NUNCA toca la BD de pruebas ni corre migraciones a mano: el server las aplica solo al
# arrancar (ARGOS_AUTO_MIGRATE=true por defecto) — respalda la BD antes si vas a probar una migración nueva.
#
# ARGOS_PUBLIC_URL/DATABASE_URL de abajo son los de esta máquina de desarrollo (LAN 192.168.100.0/24): en otra
# máquina, pásalos por variable de entorno antes de llamar al script (export ARGOS_PUBLIC_URL=...) o edítalos aquí.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin_dir="$root/.dev-bin"
log="$bin_dir/server.log"
mkdir -p "$bin_dir"

secret_key_file="$HOME/.argos-dev/secret-key.txt"

# Windows (Git Bash/MSYS) usa tasklist/taskkill y el binario lleva .exe; Linux/macOS usan pgrep/pkill.
is_windows() { [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* ]]; }
if is_windows; then bin="$bin_dir/argos-server.exe"; else bin="$bin_dir/argos-server"; fi

is_running() {
	if is_windows; then
		tasklist //FI "IMAGENAME eq argos-server.exe" 2>/dev/null | grep -qi argos-server.exe
	else
		pgrep -x argos-server >/dev/null 2>&1
	fi
}

stop_it() {
	if is_windows; then
		taskkill //F //IM argos-server.exe >/dev/null 2>&1 || true
	else
		pkill -x argos-server || true
	fi
}

case "${1:-}" in
start)
	if is_running; then
		echo "ya está corriendo; usa 'stop' primero si quieres recompilar"
		exit 0
	fi
	[[ -f "$secret_key_file" ]] || { echo "falta $secret_key_file (créala: openssl rand -hex 32 > \"$secret_key_file\")" >&2; exit 1; }

	if [[ "${2:-}" == "--web" || ! -f "$root/web/dist/index.html" ]]; then
		echo "compilando el frontend..."
		(cd "$root/web" && npm run build)
	fi
	echo "compilando el server..."
	(cd "$root/server" && go build -o "$bin" ./cmd/server)

	ARGOS_LISTEN_ADDR=0.0.0.0:8080 \
	ARGOS_PUBLIC_URL="${ARGOS_PUBLIC_URL:-http://192.168.100.18:8080}" \
	ARGOS_WEB_DIR="$root/web/dist" \
	ARGOS_SECRET_KEY="$(cat "$secret_key_file")" \
	ARGOS_AGENT_DIST_DIR="$root/dist/agent" \
	DATABASE_URL="${DATABASE_URL:-postgres://argos:argos@localhost:5432/argos?sslmode=disable}" \
	nohup "$bin" >"$log" 2>&1 &
	disown
	sleep 3
	if is_running; then
		echo "arrancado. Dashboard: http://localhost:8080"
		curl -sf http://localhost:8080/health && echo || echo "aviso: /health todavía no responde, revisa $log"
	else
		echo "no arrancó; revisa $log" >&2
		tail -n 20 "$log" >&2
		exit 1
	fi
	;;
stop)
	if is_running; then
		stop_it
		echo "detenido"
	else
		echo "no estaba corriendo"
	fi
	;;
status)
	if is_running; then
		echo "corriendo"
		curl -sf http://localhost:8080/health && echo || echo "aviso: /health no responde"
	else
		echo "detenido"
	fi
	;;
logs)
	tail -f "$log"
	;;
*)
	echo "uso: $0 start [--web] | stop | status | logs" >&2
	exit 2
	;;
esac
