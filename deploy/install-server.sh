#!/usr/bin/env bash
# Instala o actualiza el server de argos en un Linux con systemd. Ejecutar como root desde el paquete descomprimido:
#
#   sudo bash deploy/install-server.sh --public-url https://argos.lan --create-local-db
#   sudo bash deploy/install-server.sh --public-url https://argos.lan \
#        --db-url 'postgres://argos_app:CLAVE@127.0.0.1:5432/argos?sslmode=disable' \
#        --migrate-db-url 'postgres://argos_owner:CLAVE@127.0.0.1:5432/argos?sslmode=disable'
#   sudo bash deploy/install-server.sh            # actualizacion: conserva /etc/argos/*.env y la BD
#
# Primera instalacion: --public-url y una BD. Dos roles de BD (recomendado): el server opera con uno SIN privilegios
# de dueño (--db-url) y las migraciones las corre otro (--migrate-db-url, guardado 0600 solo para root).
# --create-local-db crea ambos roles (argos_owner, argos_app) y la BD "argos" en el Postgres 18 LOCAL con claves
# aleatorias. Con solo --db-url el server migra y opera con el mismo rol (modo de un solo rol, menos seguro).
# Nunca sobreescribe /etc/argos/*.env ni regenera ARGOS_SECRET_KEY.
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }
usage() { sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }

public_url="" db_url="" migrate_url="" create_db=0 listen="127.0.0.1:8080"
while [[ $# -gt 0 ]]; do
	case "$1" in
		--public-url) public_url="${2:-}"; shift 2 ;;
		--db-url) db_url="${2:-}"; shift 2 ;;
		--migrate-db-url) migrate_url="${2:-}"; shift 2 ;;
		--create-local-db) create_db=1; shift ;;
		--listen) listen="${2:-}"; shift 2 ;;
		-h|--help) usage ;;
		*) die "opcion desconocida: $1 (usa --help)" ;;
	esac
done

[[ $EUID -eq 0 ]] || die "ejecuta como root (sudo)"
command -v systemctl >/dev/null || die "hace falta systemd"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
pkg="$(cd "$here/.." && pwd)"
for f in argos-server web/index.html agent/latest deploy/argos-server.service; do
	[[ -e "$pkg/$f" ]] || die "paquete incompleto: falta $f (ejecuta el script desde el paquete descomprimido)"
done

app=/opt/argos conf=/etc/argos env=/etc/argos/argos.env menv=/etc/argos/argos-migrate.env
first=0
[[ -e "$env" ]] || first=1

if [[ $first -eq 1 ]]; then
	[[ "$public_url" =~ ^https?://[^/[:space:]]+$ ]] || die "primera instalacion: --public-url esquema://host[:puerto], sin ruta (ej. https://argos.lan)"
	[[ $create_db -eq 1 || -n "$db_url" ]] || die "primera instalacion: indica --db-url o --create-local-db"
	[[ $create_db -eq 0 || ( -z "$db_url" && -z "$migrate_url" ) ]] || die "--create-local-db excluye --db-url y --migrate-db-url"
	[[ -z "$migrate_url" || -n "$db_url" ]] || die "--migrate-db-url va acompanado de --db-url (el del rol de runtime)"
fi

id argos >/dev/null 2>&1 || useradd --system --home-dir /var/lib/argos --create-home --shell /usr/sbin/nologin argos
install -d -m 0755 "$app"
install -d -m 0750 -o root -g argos "$conf"
install -d -m 0750 -o argos -g argos /var/backups/argos

if [[ $first -eq 1 && $create_db -eq 1 ]]; then
	command -v psql >/dev/null || die "--create-local-db: no hay psql (instala postgresql 18 o usa --db-url)"
	runuser -u postgres -- psql -Atc 'SHOW server_version_num' >/dev/null 2>&1 || die "no puedo hablar con el Postgres local como usuario postgres"
	ver="$(runuser -u postgres -- psql -Atc 'SHOW server_version_num')"
	(( ver >= 180000 )) || die "argos exige Postgres 18 (el local es $ver)"
	pw_owner="$(openssl rand -hex 24)"
	pw_app="$(openssl rand -hex 24)"
	runuser -u postgres -- psql -v ON_ERROR_STOP=1 <<SQL
CREATE ROLE argos_owner LOGIN PASSWORD '$pw_owner';
CREATE ROLE argos_app LOGIN PASSWORD '$pw_app';
GRANT argos_app TO argos_owner;
CREATE DATABASE argos OWNER argos_owner;
REVOKE ALL ON DATABASE argos FROM PUBLIC;
GRANT CONNECT ON DATABASE argos TO argos_app;
SQL
	db_url="postgres://argos_app:${pw_app}@127.0.0.1:5432/argos?sslmode=disable"
	migrate_url="postgres://argos_owner:${pw_owner}@127.0.0.1:5432/argos?sslmode=disable"
fi

# Binario, frontend, binarios del agente y deploy/ se reemplazan enteros (son inmutables por version).
install -m 0755 "$pkg/argos-server" "$app/argos-server.new"
mv -f "$app/argos-server.new" "$app/argos-server"
rm -rf "$app/web" "$app/agent" "$app/deploy"
cp -a "$pkg/web" "$app/web"
cp -a "$pkg/agent" "$app/agent"
cp -a "$pkg/deploy" "$app/deploy"
chmod 0755 "$app/deploy/"*.sh
chown -R root:root "$app"

if [[ $first -eq 1 ]]; then
	secret="$(openssl rand -base64 32)"
	umask 077
	cat >"$env" <<ENV
DATABASE_URL=$db_url
ARGOS_LISTEN_ADDR=$listen
ARGOS_PUBLIC_URL=$public_url
ARGOS_TRUSTED_PROXIES=127.0.0.1/32
ARGOS_SECRET_KEY=$secret
ARGOS_WEB_DIR=$app/web
ARGOS_AGENT_DIST_DIR=$app/agent
ENV
	if [[ -n "$migrate_url" ]]; then
		# Modo de dos roles: el server no migra (lo hace `argos-admin migrate` con el rol dueño).
		echo "ARGOS_AUTO_MIGRATE=false" >>"$env"
		app_role=""
		[[ "$db_url" =~ ^postgres(ql)?://([^:@/]+)[:@] ]] && app_role="${BASH_REMATCH[2]}"
		[[ -n "$app_role" ]] || die "no pude leer el rol de runtime de --db-url"
		cat >"$menv" <<MENV
ARGOS_MIGRATE_DATABASE_URL=$migrate_url
ARGOS_APP_ROLE=$app_role
MENV
		chown root:root "$menv"
		chmod 0600 "$menv"
		echo "creado $menv (0600 solo root: contiene la clave del rol dueño del esquema)"
	fi
	chown root:argos "$env"
	chmod 0640 "$env"
	echo "creado $env (respalda este archivo fuera del server: contiene ARGOS_SECRET_KEY)"
else
	echo "conservo $env existente (no se toca)"
fi

install -m 0644 "$pkg/deploy/argos-server.service" /etc/systemd/system/argos-server.service
install -m 0644 "$pkg/deploy/argos-backup.service" /etc/systemd/system/argos-backup.service
install -m 0644 "$pkg/deploy/argos-backup.timer" /etc/systemd/system/argos-backup.timer
cat >/usr/local/bin/argos-admin <<'WRAP'
#!/usr/bin/env bash
# Corre argos-server con la config del servicio: argos-admin create-admin <usuario> | reset-password <usuario> | migrate
set -euo pipefail
[[ $EUID -eq 0 ]] || { echo "ejecuta con sudo" >&2; exit 1; }
set -a; . /etc/argos/argos.env
# migrate necesita ademas la clave del rol dueño (0600 root; el server en marcha nunca la ve).
[[ "${1:-}" == migrate && -r /etc/argos/argos-migrate.env ]] && . /etc/argos/argos-migrate.env
set +a
exec runuser -u argos --preserve-environment -- /opt/argos/argos-server "$@"
WRAP
chmod 0755 /usr/local/bin/argos-admin

systemctl daemon-reload
systemctl enable argos-server.service argos-backup.timer >/dev/null
if [[ -r "$menv" ]]; then
	echo "aplicando migraciones con el rol dueño..."
	/usr/local/bin/argos-admin migrate
fi
systemctl restart argos-server.service
systemctl start argos-backup.timer

# Espera a que responda (la primera vez aplica las migraciones).
addr="${listen:-127.0.0.1:8080}"
[[ $first -eq 1 ]] || addr="$(sed -n 's/^ARGOS_LISTEN_ADDR=//p' "$env" | head -1)"
scheme=http
grep -q '^ARGOS_TLS_CERT=' "$env" && scheme=https
ok=0
for _ in $(seq 1 30); do
	if ! systemctl is-active --quiet argos-server.service; then break; fi
	if curl -ksf -o /dev/null "$scheme://${addr/0.0.0.0/127.0.0.1}/"; then ok=1; break; fi
	sleep 1
done
if [[ $ok -ne 1 ]]; then
	journalctl -u argos-server.service -n 30 --no-pager || true
	die "el server no respondio en 30 s (log arriba)"
fi
echo "argos-server activo en $scheme://$addr"
if [[ $first -eq 1 ]]; then
	cat <<NEXT

Siguiente:
  1. Crea el admin:        sudo argos-admin create-admin admin
  2. TLS y dominio:        ver deploy/README.md (Caddy: deploy/Caddyfile.example)
  3. Respaldo diario:      ya programado (systemctl list-timers argos-backup.timer); necesita postgresql-client-18
  4. Copia fuera del server: /etc/argos/argos.env
NEXT
fi
