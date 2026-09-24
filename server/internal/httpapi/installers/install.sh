#!/bin/sh
# Instalador del agente argos (Linux/macOS). Script ESTATICO: no lleva token ni URL incrustados.
#
# Uso (como root):
#   curl --proto '=https' --tlsv1.2 -fsSL <URL>/install.sh | sh -s -- --server <URL> --token <TOKEN>
#
# Opciones:
#   --server <URL>   origen del server (https://, o http://localhost en desarrollo)
#   --token <TOKEN>  enroll token. Alternativas sin dejarlo en la lista de procesos: variable ARGOS_ENROLL_TOKEN,
#                    o no indicarlo y escribirlo cuando el script lo pida (sin eco).
#   --ca <archivo>   CA privada (PEM) del server: se usa pa las descargas y el agente la copia a su estado.
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
	server="" token="${ARGOS_ENROLL_TOKEN:-}" ca=""
	while [ $# -gt 0 ]; do
		case "$1" in
		--server | --token | --ca)
			[ $# -ge 2 ] || die "$1 necesita un valor"
			case "$1" in
			--server) server=$2 ;;
			--token) token=$2 ;;
			--ca) ca=$2 ;;
			esac
			shift 2
			;;
		*) die "argumento desconocido: $1 (opciones: --server, --token, --ca)" ;;
		esac
	done

	[ "$(id -u)" -eq 0 ] || die "ejecutalo como root (sudo)"
	[ -n "$server" ] || die "falta --server <URL>"
	server=${server%/}
	case "$server" in
	https://*) proto="=https" ;;
	http://localhost | http://localhost:* | http://127.0.0.1 | http://127.0.0.1:*) proto="=http" ;;
	*) die "el server debe ser https:// (o http://localhost en desarrollo)" ;;
	esac
	[ -z "$ca" ] || [ -r "$ca" ] || die "no se puede leer la CA: $ca"
	command -v curl >/dev/null 2>&1 || die "hace falta curl"

	if [ -z "$token" ]; then
		[ -r /dev/tty ] || die "sin token: usa --token, ARGOS_ENROLL_TOKEN o una terminal interactiva"
		printf 'Enroll token: ' >/dev/tty
		stty -echo </dev/tty
		IFS= read -r token </dev/tty || token=""
		stty echo </dev/tty
		printf '\n' >/dev/tty
	fi
	[ -n "$token" ] || die "el enroll token esta vacio"

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

	dest=/usr/local/bin
	mkdir -p "$dest"
	install -m 0755 "$tmp/agent" "$dest/argos-agent.new"
	chown 0:0 "$dest/argos-agent.new"
	mv -f "$dest/argos-agent.new" "$dest/argos-agent"

	if [ -n "$ca" ]; then
		"$dest/argos-agent" install --ca-file "$ca"
	else
		"$dest/argos-agent" install
	fi
	printf '%s\n' "$token" | "$dest/argos-agent" enroll --server "$server" --token-stdin
	token=""
	"$dest/argos-agent" start
	printf 'argos install: agente %s instalado y en marcha\n' "$version"
}

main "$@"
