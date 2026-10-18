#!/bin/sh
# Instalador del agente argos (Linux/macOS). Script ESTATICO: no lleva token ni URL incrustados.
#
# Uso (como root):
#   curl --proto '=https' --tlsv1.2 -fsSL <URL>/install.sh | sh -s -- --server <URL> --token <TOKEN>
#   curl --proto '=https' --tlsv1.2 -fsSL <URL>/install.sh | sh -s -- --update --server <URL>   (host ya enrolado)
#
# Opciones:
#   --server <URL>   origen del server (https://, o http://localhost en desarrollo)
#   --token <TOKEN>  enroll token. Alternativas sin dejarlo en la lista de procesos: variable ARGOS_ENROLL_TOKEN,
#                    o no indicarlo y escribirlo cuando el script lo pida (sin eco).
#   --ca <archivo>   CA privada (PEM) del server: se usa pa las descargas y el agente la copia a su estado.
#   --inventory <m>  modulos de inventario: ports,services,processes,updates o all (se guarda en el agente).
#   --probe-allow <redes>  IPs o CIDRs (/24 o mas chicos, separados por comas) que el colector ping puede sondear.
#   --privileged     modo privilegiado del agente (ve procesos y puertos de otros usuarios).
#   --insecure       permite un server http:// remoto (token y metricas viajan SIN CIFRAR: usa https).
#   --update         actualiza el agente ya instalado: descarga la ultima version y ejecuta `install` (conserva el
#                    enrolamiento). Exige --server; admite --ca e --insecure; no admite --token ni otras opciones.
#
# Descarga el binario, verifica su SHA-256 contra el manifiesto y ABORTA si no coincide. El token llega al
# agente por entrada estandar (enroll --token-stdin): no se escribe en disco ni en logs.
# Todo el codigo esta dentro de main() y se invoca en la ULTIMA linea: un script truncado no ejecuta nada.

set -eu

die() {
	printf 'argos install: %s\n' "$*" >&2
	exit 1
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		die "hace falta sha256sum o shasum"
	fi
}

main() {
	server="" token="${ARGOS_ENROLL_TOKEN:-}" ca="" inventory="" probe_allow="" privileged=0 insecure=0 update=0 other=0
	while [ $# -gt 0 ]; do
		case "$1" in
		--server | --token | --ca | --inventory | --probe-allow)
			[ $# -ge 2 ] || die "$1 necesita un valor"
			case "$1" in
			--server) server=$2 ;;
			--token) token=$2 ;;
			--ca) ca=$2 ;;
			--inventory) inventory=$2 ;;
			--probe-allow) probe_allow=$2 ;;
			esac
			case "$1" in --server | --ca) ;; *) other=1 ;; esac
			shift 2
			;;
		--privileged)
			privileged=1
			other=1
			shift
			;;
		--insecure)
			insecure=1
			shift
			;;
		--update)
			update=1
			shift
			;;
		*) die "argumento desconocido: $1 (opciones: --server, --token, --ca, --inventory, --probe-allow, --privileged, --insecure, --update)" ;;
		esac
	done

	[ "$(id -u)" -eq 0 ] || die "ejecutalo como root (sudo)"
	if [ "$update" -eq 1 ]; then
		# Solo --server, --ca e --insecure; el token del entorno no se usa al actualizar.
		[ "$other" -eq 0 ] || die "--update solo admite --server, --ca e --insecure (no se combina con --token ni otras opciones)"
		[ -n "$server" ] || die "--update exige --server <URL>"
		token=""
	fi
	# Lista blanca de caracteres: los valores llegan al agente como argumentos, pero se rechaza todo lo raro.
	case "$inventory" in
	*[!a-z,]*) die "--inventory solo admite letras minusculas y comas (ej. ports,services o all)" ;;
	esac
	case "$probe_allow" in
	*[!0-9a-fA-F:./,]*) die "--probe-allow solo admite IPs y CIDRs separados por comas (ej. 192.168.1.0/24)" ;;
	esac
	[ -n "$server" ] || die "falta --server <URL>"
	server=${server%/}
	case "$server" in
	https://*) proto="=https" ;;
	http://localhost | http://localhost:* | http://127.0.0.1 | http://127.0.0.1:*) proto="=http" ;;
	http://*)
		[ "$insecure" -eq 1 ] || die "el server debe ser https:// (o http://localhost en desarrollo); usa --insecure solo si aceptas que todo viaje sin cifrar"
		proto="=http"
		printf 'argos install: AVISO: server http:// sin cifrar (--insecure): el token y las metricas se pueden leer o falsificar en la red\n' >&2
		;;
	*) die "el server debe ser https:// (o http://localhost en desarrollo)" ;;
	esac
	[ -z "$ca" ] || [ -r "$ca" ] || die "no se puede leer la CA: $ca"
	command -v curl >/dev/null 2>&1 || die "hace falta curl"

	if [ "$update" -eq 0 ] && [ -z "$token" ]; then
		[ -r /dev/tty ] || die "sin token: usa --token, ARGOS_ENROLL_TOKEN o una terminal interactiva"
		printf 'Enroll token: ' >/dev/tty
		stty -echo </dev/tty
		IFS= read -r token </dev/tty || token=""
		stty echo </dev/tty
		printf '\n' >/dev/tty
	fi
	[ "$update" -eq 1 ] || [ -n "$token" ] || die "el enroll token esta vacio"

	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "sistema no soportado: $(uname -s)" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "arquitectura no soportada: $(uname -m)" ;;
	esac
	name="argos-agent_${os}_${arch}"

	tmp=$(mktemp -d) || die "no se pudo crear el directorio temporal"
	chmod 700 "$tmp"
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' INT TERM HUP

	fetch() { # fetch <url> <salida>
		if [ -n "$ca" ]; then
			curl --proto "$proto" --tlsv1.2 -fsS --cacert "$ca" -o "$2" "$1"
		else
			curl --proto "$proto" --tlsv1.2 -fsS -o "$2" "$1"
		fi
	}

	fetch "$server/dl/latest/manifest.json" "$tmp/manifest.json" || die "no se pudo descargar el manifiesto"
	version=$(sed -n 's/^"version":"\([0-9A-Za-z._+-]*\)",$/\1/p' "$tmp/manifest.json")
	want=$(sed -n "s/^{\"name\":\"$name\".*\"sha256\":\"\\([0-9a-f]\\{64\\}\\)\".*/\\1/p" "$tmp/manifest.json")
	[ -n "$version" ] && [ -n "$want" ] || die "el manifiesto no tiene $name"

	fetch "$server/dl/$version/$name" "$tmp/agent" || die "no se pudo descargar el binario"
	got=$(sha256_of "$tmp/agent")
	[ "$got" = "$want" ] || die "SHA-256 del binario no coincide con el manifiesto: abortando"

	if [ "$update" -eq 1 ]; then
		# El binario descargado se instala a si mismo: detiene el servicio, reemplaza el binario y lo rearranca
		# conservando enrolamiento y definicion del servicio.
		chmod 0700 "$tmp/agent"
		"$tmp/agent" install
		printf 'argos install: agente %s actualizado
' "$version"
		return 0
	fi

	dest=/usr/local/bin
	mkdir -p "$dest"
	install -m 0755 "$tmp/agent" "$dest/argos-agent.new"
	chown 0:0 "$dest/argos-agent.new"
	mv -f "$dest/argos-agent.new" "$dest/argos-agent"

	set -- install
	[ -z "$ca" ] || set -- "$@" --ca-file "$ca"
	[ -z "$inventory" ] || set -- "$@" --inventory "$inventory"
	[ -z "$probe_allow" ] || set -- "$@" --probe-allow "$probe_allow"
	[ "$privileged" -eq 0 ] || set -- "$@" --privileged
	[ "$insecure" -eq 0 ] || set -- "$@" --insecure
	"$dest/argos-agent" "$@"
	printf '%s\n' "$token" | "$dest/argos-agent" enroll --server "$server" --token-stdin
	token=""
	"$dest/argos-agent" start
	printf 'argos install: agente %s instalado y en marcha\n' "$version"
}

main "$@"
