#!/usr/bin/env bash
# Instala o actualiza el server de argos en un Linux con systemd. Ejecutar como root desde el paquete descomprimido.
#
# Un solo comando en un Debian/Ubuntu limpio (Postgres 18 + BD + HTTPS con CA interna + primer admin):
#   sudo bash deploy/install-server.sh --public-url https://argos.lan --install-postgres --create-local-db \
#        --with-caddy --tls-internal --admin-user admin
# Con dominio publico (Let's Encrypt automatico; los puertos 80 y 443 deben llegar a este equipo):
#   sudo bash deploy/install-server.sh --domain argos.midominio.com --install-postgres --create-local-db --with-caddy
# Con una BD existente (dos roles recomendado) y sin proxy:
#   sudo bash deploy/install-server.sh --public-url https://argos.lan \
#        --db-url 'postgres://argos_app:CLAVE@127.0.0.1:5432/argos?sslmode=disable' \
#        --migrate-db-url 'postgres://argos_owner:CLAVE@127.0.0.1:5432/argos?sslmode=disable'
# Actualizacion (conserva /etc/argos/*.env y la BD):   sudo bash deploy/install-server.sh
# Revisar sin tocar nada (no exige root):              bash deploy/install-server.sh --dry-run <mismas opciones>
#
# Opciones: --public-url URL | --domain NOMBRE | --db-url URL [--migrate-db-url URL] | --create-local-db |
#   --install-postgres | --with-caddy (--domain NOMBRE | --tls-internal) | --admin-user NOMBRE (def. admin) |
#   --no-admin | --listen HOST:PUERTO (def. 127.0.0.1:8080) | --dry-run
# Todo es idempotente: re-ejecutar no duplica ni rompe nada. Nunca sobreescribe /etc/argos/*.env ni regenera
# ARGOS_SECRET_KEY. La clave del admin inicial se imprime UNA vez al final (no se guarda en ningun archivo ni log).
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }
usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; exit 2; }

DRY=0
# --- utilidades (dry-run: imprimen lo que harian y no ejecutan) ---------------------------------------------------
step() { echo; echo "==> $*"; }
# sys CMD ARGS...: ejecuta un comando que toca el sistema (o solo lo imprime con --dry-run).
sys() {
	if [[ $DRY -eq 1 ]]; then printf '  [sistema] %s\n' "$*"; else "$@"; fi
}
# put_file MODO DUENO:GRUPO DESTINO [oculto]: escribe stdin en DESTINO de forma atomica. Con --dry-run muestra el
# contenido salvo que se marque "oculto" (secretos).
put_file() {
	local mode="$1" owner="$2" dest="$3" hide="${4:-}" tmp
	if [[ $DRY -eq 1 ]]; then
		printf '  [sistema] escribir %s (modo %s, %s)\n' "$dest" "$mode" "$owner"
		if [[ "$hide" == oculto ]]; then cat >/dev/null; echo "    (contenido con secretos, no se muestra)"; else sed 's/^/    | /'; fi
		return 0
	fi
	tmp="$(umask 077; mktemp "$dest.XXXXXX")"
	cat >"$tmp"
	chown "$owner" "$tmp"; chmod "$mode" "$tmp"
	mv -f "$tmp" "$dest"
}

# --- funciones puras (probadas con mocks; sin efectos) ------------------------------------------------------------
# URL publica: esquema://host[:puerto], host con lista blanca [A-Za-z0-9.-] (nombre o IPv4), sin ruta.
valid_public_url() {
	[[ "$1" =~ ^https?://[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:[0-9]{1,5})?$ ]] || return 1
	[[ "${1#*://}" == *:* ]] || return 0   # sin puerto explicito
	local p="${1##*:}"
	(( 10#$p >= 1 && 10#$p <= 65535 ))
}
valid_domain() { [[ "$1" =~ ^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$ && ${#1} -le 253 ]]; }
valid_admin_user() { [[ "$1" =~ ^[A-Za-z0-9._@+-]{3,64}$ ]]; }   # igual que auth.ValidateUsername
url_scheme() { echo "${1%%://*}"; }
url_authority() { echo "${1#*://}"; }
url_host() { local a; a="$(url_authority "$1")"; echo "${a%%:*}"; }
url_port() { local a; a="$(url_authority "$1")"; if [[ "$a" == *:* ]]; then echo "${a##*:}"; fi; }
# Contrasena aleatoria fuerte: openssl -> base64 sin +/= (>= 30 caracteres alfanumericos; la politica pide >= 15).
gen_password() {
	local pw
	pw="$(openssl rand -base64 33 | tr -d '+/=\n')"
	[[ ${#pw} -ge 30 ]] || return 1
	echo "$pw"
}
# render_caddyfile SITIO PUERTO_UPSTREAM (tls-internal|auto)
render_caddyfile() {
	local site="$1" upstream_port="$2" tls="$3"
	echo "# Gestionado por argos (deploy/install-server.sh): se regenera al re-ejecutar el instalador."
	echo "$site {"
	if [[ "$tls" == tls-internal ]]; then printf '\ttls internal\n'; fi
	printf '\tencode zstd gzip\n\treverse_proxy 127.0.0.1:%s\n}\n' "$upstream_port"
}
# Sitio de Caddy a partir de la URL publica: host, o host:puerto si el puerto no es el 443.
caddy_site() {
	local h p; h="$(url_host "$1")"; p="$(url_port "$1")"
	if [[ -n "$p" && "$p" != 443 ]]; then echo "$h:$p"; else echo "$h"; fi
}
# set_env_kv ARCHIVO CLAVE VALOR: reemplaza o agrega CLAVE=VALOR (VALOR ya validado con lista blanca: sin | & \ ni saltos).
set_env_kv() {
	local f="$1" k="$2" v="$3"
	if [[ $DRY -eq 1 ]]; then printf '  [sistema] fijar %s=%s en %s\n' "$k" "$v" "$f"; return 0; fi
	if grep -q "^$k=" "$f"; then sed -i "s|^$k=.*|$k=$v|" "$f"; else echo "$k=$v" >>"$f"; fi
}

# wait_health URL SEGUNDOS [args extra de curl]: reintenta cada segundo.
wait_health() {
	local url="$1" secs="$2" i; shift 2
	for ((i = 0; i < secs; i++)); do
		curl -ksf -o /dev/null --max-time 3 "$@" "$url" && return 0
		systemctl is-active --quiet argos-server.service || return 1
		sleep 1
	done
	return 1
}

# has_admin_check VAR: pone yes|no|unknown en VAR. Consulta la BD con el DATABASE_URL del env (rol de runtime).
has_admin_check() {
	local -n out=$1
	local url n
	command -v psql >/dev/null 2>&1 || { out=unknown; return 0; }
	url="$(sed -n 's/^DATABASE_URL=//p' "${ARGOS_CONF_DIR:-/etc/argos}/argos.env" | head -1)"
	if n="$(psql "$url" -Atc "SELECT count(*) FROM users WHERE role = 'admin'" 2>/dev/null)" && [[ "$n" =~ ^[0-9]+$ ]]; then
		if (( n > 0 )); then out=yes; else out=no; fi
	else
		out=unknown
	fi
}

print_summary() {
	local url="$1" first="$2" user="$3" made="$4" pw="$5" ca="$6" caddy="$7" env="$8" menv="$9" noadmin="${10}"
	echo
	echo "=============================================================="
	echo " argos instalado"
	echo "=============================================================="
	echo " URL:              $url"
	if [[ $noadmin -eq 0 ]]; then echo " Usuario admin:    $user"; fi
	echo " Archivos:         /opt/argos (binarios, web, agentes); config $env${menv:+ y $menv (0600)}"
	echo " Logs:             journalctl -u argos-server -f"
	if [[ $caddy -eq 1 ]]; then echo "                   journalctl -u caddy -f"; fi
	echo " Estado:           systemctl status argos-server"
	echo " Actualizar:       descomprime el paquete nuevo y corre: sudo bash deploy/install-server.sh"
	echo "                   (antes, respaldo: sudo systemctl start argos-backup.service)"
	echo " Respaldo diario:  ya programado (systemctl list-timers argos-backup.timer)"
	echo " IMPORTANTE:       copia $env FUERA del server (contiene ARGOS_SECRET_KEY;"
	echo "                   sin ella los canales de notificacion cifrados no se pueden leer)"
	if [[ -n "$ca" ]]; then
		echo
		echo " CA interna de Caddy: $ca"
		echo "   Navegadores: copia ese archivo a tu equipo (scp) e instalalo como autoridad raiz de confianza."
		echo "   Agentes:     pasa el archivo con  argos-agent install ... --ca-file root.crt"
	fi
	if [[ $made -eq 1 ]]; then
		echo
		echo "  +----------------------------------------------------------+"
		echo "  |  CLAVE INICIAL DEL ADMIN (se muestra UNA sola vez)       |"
		echo "  |                                                          |"
		printf '  |  usuario: %-47s|\n' "$user"
		printf '  |  clave:   %-47s|\n' "$pw"
		echo "  |                                                          |"
		echo "  |  Guardala ya. Recomendado: cambiala en el primer login   |"
		echo "  |  (el CLI aun no fuerza el cambio de clave inicial).      |"
		echo "  +----------------------------------------------------------+"
	elif [[ $first -eq 1 && $noadmin -eq 1 ]]; then
		echo " Crea el admin:    sudo argos-admin create-admin NOMBRE"
	fi
	if [[ $caddy -eq 0 ]]; then echo " HTTPS:            no configurado aqui (--with-caddy o deploy/README.md); sin HTTPS el login solo funciona desde localhost"; fi
}

# --- programa principal -------------------------------------------------------------------------------------------
main() {
	local public_url="" db_url="" migrate_url="" create_db=0 listen="127.0.0.1:8080" install_pg=0 with_caddy=0
	local domain="" tls_internal=0 admin_user="admin" admin_set=0 no_admin=0
	while [[ $# -gt 0 ]]; do
		case "$1" in
			--public-url) public_url="${2:-}"; shift 2 ;;
			--db-url) db_url="${2:-}"; shift 2 ;;
			--migrate-db-url) migrate_url="${2:-}"; shift 2 ;;
			--create-local-db) create_db=1; shift ;;
			--install-postgres) install_pg=1; shift ;;
			--with-caddy) with_caddy=1; shift ;;
			--domain) domain="${2:-}"; shift 2 ;;
			--tls-internal) tls_internal=1; shift ;;
			--admin-user) admin_user="${2:-}"; admin_set=1; shift 2 ;;
			--no-admin) no_admin=1; shift ;;
			--listen) listen="${2:-}"; shift 2 ;;
			--dry-run) DRY=1; shift ;;
			-h|--help) usage ;;
			*) die "opcion desconocida: $1 (usa --help)" ;;
		esac
	done

	local app=/opt/argos conf="${ARGOS_CONF_DIR:-/etc/argos}"   # ARGOS_CONF_DIR: solo para pruebas
	local env="$conf/argos.env" menv="$conf/argos-migrate.env" first=0
	[[ -e "$env" ]] || first=1

	# Validaciones con lista blanca (antes de tocar nada).
	[[ $no_admin -eq 0 || $admin_set -eq 0 ]] || die "--admin-user y --no-admin se excluyen"
	valid_admin_user "$admin_user" || die "--admin-user: 3-64 caracteres entre letras, numeros y . _ @ + -"
	if [[ -n "$domain" ]]; then
		valid_domain "$domain" || die "--domain: nombre de dominio invalido (ej. argos.midominio.com)"
		[[ $with_caddy -eq 1 ]] || die "--domain se usa con --with-caddy"
		[[ -n "$public_url" ]] || public_url="https://$domain"
		[[ "$(url_host "$public_url")" == "$domain" ]] || die "--domain ($domain) debe coincidir con el host de --public-url"
	fi
	if [[ $with_caddy -eq 1 ]]; then
		[[ -n "$domain" || $tls_internal -eq 1 ]] || die "--with-caddy exige --domain NOMBRE (Let's Encrypt) o --tls-internal (CA interna)"
		[[ -z "$domain" || $tls_internal -eq 0 ]] || die "--domain y --tls-internal se excluyen"
	else
		[[ $tls_internal -eq 0 ]] || die "--tls-internal se usa con --with-caddy"
	fi
	[[ "$listen" =~ ^[A-Za-z0-9.-]+:[0-9]{1,5}$ ]] || die "--listen: HOST:PUERTO"
	local listen_port="${listen##*:}"
	if [[ $first -eq 1 ]]; then
		valid_public_url "$public_url" || die "primera instalacion: --public-url esquema://host[:puerto], sin ruta (ej. https://argos.lan)"
		[[ $create_db -eq 1 || -n "$db_url" ]] || die "primera instalacion: indica --db-url o --create-local-db"
		[[ $create_db -eq 0 || ( -z "$db_url" && -z "$migrate_url" ) ]] || die "--create-local-db excluye --db-url y --migrate-db-url"
		[[ -z "$migrate_url" || -n "$db_url" ]] || die "--migrate-db-url va acompanado de --db-url (el del rol de runtime)"
	else
		[[ -z "$public_url" ]] || valid_public_url "$public_url" || die "--public-url invalida"
		if [[ $with_caddy -eq 1 && -z "$public_url" ]]; then   # re-ejecucion: toma la URL del env existente
			public_url="$(sed -n 's/^ARGOS_PUBLIC_URL=//p' "$env" | head -1)"
			valid_public_url "$public_url" || die "no pude leer un ARGOS_PUBLIC_URL valido de $env"
		fi
		if [[ $create_db -eq 1 || -n "$db_url" || -n "$migrate_url" ]]; then
			echo "aviso: $env ya existe: se ignoran --create-local-db/--db-url/--migrate-db-url"
		fi
		create_db=0
	fi
	if [[ -z "$public_url" && $first -eq 0 ]]; then public_url="$(sed -n 's/^ARGOS_PUBLIC_URL=//p' "$env" | head -1)"; fi
	if [[ $with_caddy -eq 1 ]]; then
		[[ "$(url_scheme "$public_url")" == https ]] || die "--with-caddy exige una URL publica https://..."
		[[ -z "$domain" || "$(url_host "$public_url")" == "$domain" ]] || die "--domain ($domain) debe coincidir con el host de la URL publica"
	fi

	if [[ $DRY -eq 1 ]]; then
		echo "MODO --dry-run: solo se imprime lo que se haria; no se ejecuta nada ni se exige root. [sistema] = tocaria el sistema."
	else
		[[ $EUID -eq 0 ]] || die "ejecuta como root (sudo)"
		command -v systemctl >/dev/null || die "hace falta systemd"
		command -v openssl >/dev/null || die "hace falta openssl"
	fi
	local here pkg f
	here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
	pkg="$(cd "$here/.." && pwd)"
	for f in argos-server web/index.html agent/latest deploy/argos-server.service deploy/argos-admin; do
		if [[ ! -e "$pkg/$f" ]]; then
			if [[ $DRY -eq 1 ]]; then echo "aviso (dry-run): falta $f en el paquete"
			else die "paquete incompleto: falta $f (ejecuta el script desde el paquete descomprimido)"; fi
		fi
	done
	if [[ $first -eq 1 ]]; then echo "modo: PRIMERA instalacion ($env no existe)"; else echo "modo: ACTUALIZACION ($env existe: no se toca)"; fi

	# ---- 1. PostgreSQL 18 ----
	local pg_port=5432
	psql_pg() { runuser -u postgres -- env PGPORT="$pg_port" psql "$@"; }
	if command -v pg_lsclusters >/dev/null 2>&1; then
		local p18; p18="$(pg_lsclusters -h 2>/dev/null | awk '$1==18{print $3; exit}')"
		if [[ -n "$p18" ]]; then pg_port="$p18"; fi
	fi
	if [[ $install_pg -eq 1 ]]; then
		step "PostgreSQL 18"
		local pgver=0
		if [[ $DRY -eq 0 ]] && command -v psql >/dev/null 2>&1; then
			pgver="$(psql_pg -Atc 'SHOW server_version_num' 2>/dev/null || echo 0)"
		fi
		if (( pgver >= 180000 )); then
			echo "ya hay PostgreSQL 18 en el puerto $pg_port: no se reinstala"
		else
			local id="" idlike=""
			if [[ -r /etc/os-release ]]; then id="$(. /etc/os-release; echo "${ID:-}")"; idlike="$(. /etc/os-release; echo "${ID_LIKE:-}")"; fi
			if [[ $DRY -eq 0 && "$id $idlike" != *debian* && "$id $idlike" != *ubuntu* ]]; then
				die "--install-postgres solo automatiza Debian/Ubuntu (esta es '${id:-desconocida}'). Instala PostgreSQL 18 a mano (https://www.postgresql.org/download/linux/) y usa --create-local-db o --db-url sin --install-postgres"
			fi
			sys apt-get update -qq
			sys env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq curl ca-certificates gnupg openssl
			sys install -d -m 0755 /usr/share/postgresql-common/pgdg
			# Clave oficial de PGDG, descargada y referenciada con signed-by (no apt-key ni curl|sh).
			sys curl -fsSL -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc
			if [[ $DRY -eq 1 ]]; then
				echo '  [sistema] escribir /etc/apt/sources.list.d/pgdg.list: deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt <codename>-pgdg main'
			else
				local codename; codename="$(. /etc/os-release; echo "${VERSION_CODENAME:-}")"
				[[ "$codename" =~ ^[a-z0-9]+$ ]] || die "no pude leer VERSION_CODENAME de /etc/os-release"
				echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt ${codename}-pgdg main" \
					| put_file 0644 root:root /etc/apt/sources.list.d/pgdg.list
			fi
			sys apt-get update -qq
			sys env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq postgresql-18 postgresql-client-18
			sys systemctl enable --now postgresql
			if [[ $DRY -eq 0 ]]; then
				# Si ya habia un cluster mas viejo en el 5432, Debian deja el 18 en otro puerto: se detecta.
				local p18b v; p18b="$(pg_lsclusters -h 2>/dev/null | awk '$1==18{print $3; exit}')"
				if [[ -n "$p18b" ]]; then pg_port="$p18b"; fi
				if [[ "$pg_port" != 5432 ]]; then echo "aviso: el cluster 18 quedo en el puerto $pg_port (hay otro Postgres en el 5432); se usara ese puerto"; fi
				v="$(psql_pg -Atc 'SHOW server_version_num' 2>/dev/null || echo 0)"
				(( v >= 180000 )) || die "tras instalar, SHOW server_version_num = $v (se esperaba >= 180000)"
				echo "PostgreSQL 18 listo (server_version_num=$v, puerto $pg_port)"
			fi
		fi
	fi

	# ---- 2. Usuario, directorios y BD local ----
	step "Usuario de sistema y directorios"
	if [[ $DRY -eq 1 ]] || ! id argos >/dev/null 2>&1; then
		sys useradd --system --home-dir /var/lib/argos --create-home --shell /usr/sbin/nologin argos
	fi
	sys install -d -m 0755 "$app"
	sys install -d -m 0750 -o root -g argos "$conf"
	sys install -d -m 0750 -o argos -g argos /var/backups/argos

	if [[ $first -eq 1 && $create_db -eq 1 ]]; then
		step "BD local: roles argos_owner/argos_app y base argos"
		local pw_owner pw_app
		if [[ $DRY -eq 1 ]]; then
			echo "  [sistema] (como usuario postgres, puerto $pg_port) CREATE ROLE argos_owner y argos_app con claves aleatorias; CREATE DATABASE argos OWNER argos_owner; REVOKE/GRANT CONNECT"
			pw_owner="<clave-owner>"; pw_app="<clave-app>"
		else
			command -v psql >/dev/null || die "--create-local-db: no hay psql (usa --install-postgres o instala postgresql 18)"
			psql_pg -Atc 'SHOW server_version_num' >/dev/null 2>&1 || die "no puedo hablar con el Postgres local como usuario postgres (puerto $pg_port)"
			local ver; ver="$(psql_pg -Atc 'SHOW server_version_num')"
			(( ver >= 180000 )) || die "argos exige Postgres 18 (el local es $ver; usa --install-postgres)"
			# Si una corrida anterior fallo a medias, los roles/la BD pueden existir: no se pisan.
			[[ "$(psql_pg -Atc "SELECT count(*) FROM pg_roles WHERE rolname IN ('argos_owner','argos_app')")" == 0 \
				&& "$(psql_pg -Atc "SELECT count(*) FROM pg_database WHERE datname = 'argos'")" == 0 ]] \
				|| die "ya existen roles argos_owner/argos_app o la BD argos en este Postgres pero no hay $env: borralos (DROP DATABASE argos; DROP ROLE argos_owner, argos_app) o usa --db-url"
			pw_owner="$(openssl rand -hex 24)"
			pw_app="$(openssl rand -hex 24)"
			# Las claves son hex (sin comillas): seguras dentro del SQL. Se envian por stdin, no por argumento.
			printf '%s\n' \
				"CREATE ROLE argos_owner LOGIN PASSWORD '$pw_owner';" \
				"CREATE ROLE argos_app LOGIN PASSWORD '$pw_app';" \
				"GRANT argos_app TO argos_owner;" \
				"CREATE DATABASE argos OWNER argos_owner;" \
				"REVOKE ALL ON DATABASE argos FROM PUBLIC;" \
				"GRANT CONNECT ON DATABASE argos TO argos_app;" \
				| psql_pg -q -v ON_ERROR_STOP=1 >/dev/null
		fi
		db_url="postgres://argos_app:${pw_app}@127.0.0.1:${pg_port}/argos?sslmode=disable"
		migrate_url="postgres://argos_owner:${pw_owner}@127.0.0.1:${pg_port}/argos?sslmode=disable"
	fi

	# ---- 3. Archivos ----
	step "Binario, web, agentes y deploy/ en $app"
	if [[ $DRY -eq 1 ]]; then
		echo "  [sistema] reemplazar $app/argos-server, $app/web, $app/agent y $app/deploy por los del paquete"
	else
		# Binario, frontend, binarios del agente y deploy/ se reemplazan enteros (son inmutables por version).
		install -m 0755 "$pkg/argos-server" "$app/argos-server.new"
		mv -f "$app/argos-server.new" "$app/argos-server"
		rm -rf "$app/web" "$app/agent" "$app/deploy"
		cp -a "$pkg/web" "$app/web"
		cp -a "$pkg/agent" "$app/agent"
		cp -a "$pkg/deploy" "$app/deploy"
		chmod 0755 "$app/deploy/"*.sh
		chown -R root:root "$app"
	fi

	# ---- 4. Configuracion ----
	step "Configuracion ($env)"
	if [[ $first -eq 1 ]]; then
		local secret app_role=""
		if [[ $DRY -eq 1 ]]; then secret="<ARGOS_SECRET_KEY-aleatoria>"; else secret="$(openssl rand -base64 32)"; fi
		{
			echo "DATABASE_URL=$db_url"
			echo "ARGOS_LISTEN_ADDR=$listen"
			echo "ARGOS_PUBLIC_URL=$public_url"
			echo "ARGOS_TRUSTED_PROXIES=127.0.0.1/32"
			echo "ARGOS_SECRET_KEY=$secret"
			echo "ARGOS_WEB_DIR=$app/web"
			echo "ARGOS_AGENT_DIST_DIR=$app/agent"
			# Modo de dos roles: el server no migra (lo hace `argos-admin migrate` con el rol dueño).
			if [[ -n "$migrate_url" ]]; then echo "ARGOS_AUTO_MIGRATE=false"; fi
		} | put_file 0640 root:argos "$env" oculto
		if [[ -n "$migrate_url" ]]; then
			if [[ "$db_url" =~ ^postgres(ql)?://([^:@/]+)[:@] ]]; then app_role="${BASH_REMATCH[2]}"; fi
			[[ -n "$app_role" ]] || die "no pude leer el rol de runtime de --db-url"
			printf 'ARGOS_MIGRATE_DATABASE_URL=%s\nARGOS_APP_ROLE=%s\n' "$migrate_url" "$app_role" | put_file 0600 root:root "$menv" oculto
			echo "$menv es 0600 solo root: contiene la clave del rol dueño del esquema"
		fi
		echo "$env creado: respalda este archivo fuera del server (contiene ARGOS_SECRET_KEY)"
	else
		echo "conservo $env existente (no se toca)"
	fi

	# ---- 5. Caddy (HTTPS) ----
	local site="" proxy_ca=""
	if [[ $with_caddy -eq 1 ]]; then
		step "Caddy: HTTPS para $(url_host "$public_url")"
		if [[ $DRY -eq 1 ]] || ! command -v caddy >/dev/null 2>&1; then
			sys apt-get update -qq
			sys env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq debian-keyring debian-archive-keyring apt-transport-https curl gnupg
			# Clave oficial de Caddy: descargada, convertida y referenciada con signed-by (no curl|sh).
			if [[ $DRY -eq 1 ]]; then
				echo '  [sistema] curl -fsSL https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --dearmor -> /usr/share/keyrings/caddy-stable-archive-keyring.gpg'
				echo '  [sistema] escribir /etc/apt/sources.list.d/caddy-stable.list: deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main'
			else
				local gk; gk="$(mktemp)"
				curl -fsSL -o "$gk" https://dl.cloudsmith.io/public/caddy/stable/gpg.key
				gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg "$gk"
				rm -f "$gk"
				chmod 0644 /usr/share/keyrings/caddy-stable-archive-keyring.gpg
				echo "deb [signed-by=/usr/share/keyrings/caddy-stable-archive-keyring.gpg] https://dl.cloudsmith.io/public/caddy/stable/deb/debian any-version main" \
					| put_file 0644 root:root /etc/apt/sources.list.d/caddy-stable.list
			fi
			sys apt-get update -qq
			sys env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq caddy
		else
			echo "Caddy ya instalado"
		fi
		site="$(caddy_site "$public_url")"
		local cmode=auto cf=/etc/caddy/Caddyfile ct
		if [[ $tls_internal -eq 1 ]]; then cmode=tls-internal; fi
		if [[ $DRY -eq 1 ]]; then
			echo "  [sistema] si $cf existe y no es de argos, copia de seguridad en $cf.bak-argos"
			render_caddyfile "$site" "$listen_port" "$cmode" | put_file 0644 root:root "$cf"
			echo "  [sistema] caddy validate --config <temporal> --adapter caddyfile; systemctl enable --now caddy; systemctl reload caddy"
		else
			ct="$(mktemp)"
			render_caddyfile "$site" "$listen_port" "$cmode" >"$ct"
			caddy validate --config "$ct" --adapter caddyfile >/dev/null 2>&1 || { rm -f "$ct"; die "caddy validate rechazo el Caddyfile generado"; }
			if [[ -e "$cf" ]] && ! grep -q '^# Gestionado por argos' "$cf" && [[ ! -e "$cf.bak-argos" ]]; then
				cp -a "$cf" "$cf.bak-argos"; echo "copia del Caddyfile previo en $cf.bak-argos"
			fi
			put_file 0644 root:root "$cf" <"$ct"
			rm -f "$ct"
			systemctl enable --now caddy >/dev/null 2>&1 || true
			systemctl reload caddy || systemctl restart caddy
			echo "Caddyfile listo: $site -> 127.0.0.1:$listen_port"
		fi
		if [[ $first -eq 0 ]]; then
			# Alinea el env existente con el proxy (solo lo necesario).
			set_env_kv "$env" ARGOS_PUBLIC_URL "$public_url"
			set_env_kv "$env" ARGOS_TRUSTED_PROXIES 127.0.0.1/32
		fi
		# Firewall: solo ufw y solo si esta activo; con otros, avisar.
		if [[ $DRY -eq 1 ]]; then
			echo "  [sistema] si ufw esta activo: ufw allow 80/tcp y 443/tcp (otros firewalls no se tocan, solo se avisa)"
		elif command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
			ufw allow 80/tcp
			ufw allow 443/tcp
		else
			echo "aviso: ufw no esta activo; si usas otro firewall (nftables, firewalld, grupos de seguridad de la nube), abre 80 y 443/tcp tu mismo"
		fi
		if [[ $tls_internal -eq 1 ]]; then proxy_ca=/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt; fi
	fi

	# ---- 6. systemd, wrapper argos-admin y arranque ----
	step "Servicio systemd, respaldo diario y comando argos-admin"
	if [[ $DRY -eq 1 ]]; then
		echo "  [sistema] instalar argos-server.service, argos-backup.service y argos-backup.timer en /etc/systemd/system"
		echo "  [sistema] instalar deploy/argos-admin en /usr/local/bin/argos-admin (create-admin | reset-password | migrate)"
		echo "  [sistema] systemctl daemon-reload; enable argos-server.service y argos-backup.timer"
		if [[ $first -eq 1 || -e "$menv" ]]; then echo "  [sistema] argos-admin migrate (migraciones con el rol dueño, si hay $menv)"; fi
		echo "  [sistema] systemctl restart argos-server.service; start argos-backup.timer"
	else
		install -m 0644 "$pkg/deploy/argos-server.service" /etc/systemd/system/argos-server.service
		install -m 0644 "$pkg/deploy/argos-backup.service" /etc/systemd/system/argos-backup.service
		install -m 0644 "$pkg/deploy/argos-backup.timer" /etc/systemd/system/argos-backup.timer
		install -m 0755 "$pkg/deploy/argos-admin" /usr/local/bin/argos-admin
		systemctl daemon-reload
		systemctl enable argos-server.service argos-backup.timer >/dev/null
		if [[ -r "$menv" ]]; then
			echo "aplicando migraciones con el rol dueño..."
			/usr/local/bin/argos-admin migrate
		fi
		systemctl restart argos-server.service
		systemctl start argos-backup.timer
	fi

	# ---- 7. Verificacion: /health directo y por el proxy ----
	step "Verificacion de /health"
	local addr scheme=http
	if [[ $DRY -eq 1 ]]; then
		echo "  [sistema] esperar hasta 30 s a http://$listen/health (directo)"
		if [[ $with_caddy -eq 1 ]]; then echo "  [sistema] esperar hasta 60 s a $public_url/health por Caddy (curl --resolve, -k por la CA interna)"; fi
	else
		addr="$listen"
		if [[ $first -eq 0 ]]; then addr="$(sed -n 's/^ARGOS_LISTEN_ADDR=//p' "$env" | head -1)"; fi
		if grep -q '^ARGOS_TLS_CERT=' "$env"; then scheme=https; fi
		wait_health "$scheme://${addr/0.0.0.0/127.0.0.1}/health" 30 || {
			journalctl -u argos-server.service -n 30 --no-pager || true
			die "el server no respondio en 30 s (log arriba)"
		}
		echo "argos-server responde en $scheme://$addr"
		if [[ $with_caddy -eq 1 ]]; then
			local ph pp; ph="$(url_host "$public_url")"; pp="$(url_port "$public_url")"; pp="${pp:-443}"
			wait_health "$public_url/health" 60 --resolve "$ph:$pp:127.0.0.1" || {
				journalctl -u caddy -n 20 --no-pager || true
				die "el server responde pero Caddy no sirve $public_url/health en 60 s (log de caddy arriba)"
			}
			echo "HTTPS operativo: $public_url"
		fi
	fi

	# ---- 8. Primer admin ----
	local admin_pw="" admin_made=0
	if [[ $no_admin -eq 0 ]]; then
		step "Primer administrador ($admin_user)"
		if [[ $DRY -eq 1 ]]; then
			echo "  [sistema] si no hay ningun admin en la BD: generar clave aleatoria (openssl rand), pasarla por stdin a 'argos-admin create-admin $admin_user' e imprimirla una sola vez al final"
		else
			local has=unknown errf
			has_admin_check has
			if [[ "$has" == yes ]]; then
				echo "ya existe un administrador: no se crea otro"
			elif [[ "$has" == unknown && $first -eq 0 ]]; then
				echo "no pude comprobar si hay admin (falta psql) y no es primera instalacion: se omite. Crea uno con: sudo argos-admin create-admin NOMBRE"
			else
				admin_pw="$(gen_password)" || die "no pude generar la clave del admin"
				errf="$(mktemp)"
				# La clave viaja por stdin (no por argumento ni entorno) y no se escribe en ningun archivo.
				if printf '%s\n' "$admin_pw" | /usr/local/bin/argos-admin create-admin "$admin_user" 2>"$errf"; then
					admin_made=1
				else
					cat "$errf" >&2; rm -f "$errf"
					die "no se pudo crear el admin '$admin_user' (la clave no se guarda; usa: sudo argos-admin create-admin NOMBRE)"
				fi
				rm -f "$errf"
			fi
		fi
	fi

	# ---- 9. Resumen ----
	if [[ $DRY -eq 1 ]]; then echo; echo "--dry-run terminado: no se ejecuto nada."; return 0; fi
	local menv_show=""
	if [[ -e "$menv" || ( $DRY -eq 1 && -n "$migrate_url" ) ]]; then menv_show="$menv"; fi
	print_summary "$public_url" "$first" "$admin_user" "$admin_made" "$admin_pw" "$proxy_ca" "$with_caddy" "$env" "$menv_show" "$no_admin"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
