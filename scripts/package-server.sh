#!/usr/bin/env bash
# Empaqueta el server pa Linux: binario + frontend + binarios del agente + deploy/ en un .tar.gz por arquitectura.
#
# Uso: bash scripts/package-server.sh <version> [--build-web] [--agent-dist <dir>]
#   version        ej. 0.7.0 (letras, digitos, . _ + -)
#   --build-web    corre `npm ci && npm run build` antes (por defecto usa web/dist ya construido)
#   --agent-dist   directorio de binarios del agente (por defecto dist/agent; se incluye la version de `latest`)
#
# Salida: dist/server/argos-server_<version>_linux_<amd64|arm64>.tar.gz (+ .sha256). Instalar: ver deploy/README.md.
set -euo pipefail

version="${1:-}"
if [[ ! "$version" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]]; then
	echo "uso: $0 <version> [--build-web] [--agent-dist <dir>]  (version invalida o vacia)" >&2
	exit 2
fi
shift
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
build_web=0
agent_dist="$root/dist/agent"
while [[ $# -gt 0 ]]; do
	case "$1" in
		--build-web) build_web=1; shift ;;
		--agent-dist) agent_dist="${2:?falta el directorio}"; shift 2 ;;
		*) echo "opcion desconocida: $1" >&2; exit 2 ;;
	esac
done

if [[ $build_web -eq 1 ]]; then
	(cd "$root/web" && npm ci && npm run build)
fi
[[ -f "$root/web/dist/index.html" ]] || { echo "falta web/dist (usa --build-web)" >&2; exit 1; }
[[ -f "$agent_dist/latest" ]] || { echo "falta $agent_dist/latest (bash scripts/build-agent.sh <version>)" >&2; exit 1; }
agent_ver="$(tr -d '[:space:]' <"$agent_dist/latest")"
[[ -f "$agent_dist/$agent_ver/manifest.json" ]] || { echo "falta $agent_dist/$agent_ver/manifest.json" >&2; exit 1; }

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d ' ' -f 1; else shasum -a 256 "$1" | cut -d ' ' -f 1; fi
}

out="$root/dist/server"
mkdir -p "$out"
for arch in amd64 arm64; do
	name="argos-server_${version}_linux_${arch}"
	stage="$(mktemp -d)"
	pkg="$stage/$name"
	mkdir -p "$pkg/agent"
	echo "compilando $name"
	(cd "$root/server" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.version=${version}" -o "$pkg/argos-server" ./cmd/server)
	cp -a "$root/web/dist" "$pkg/web"
	cp -a "$agent_dist/latest" "$pkg/agent/latest"
	cp -a "$agent_dist/$agent_ver" "$pkg/agent/$agent_ver"
	cp -a "$root/deploy" "$pkg/deploy"
	# Los scripts y unidades se ejecutan en Linux: fuera CRLF aunque el arbol de trabajo sea de Windows.
	for f in "$pkg"/deploy/*; do sed -i 's/\r$//' "$f"; done
	chmod 0755 "$pkg/argos-server" "$pkg"/deploy/*.sh
	printf '%s\n' "$version" >"$pkg/VERSION"
	tar -C "$stage" -czf "$out/$name.tar.gz" "$name"
	echo "$(sha256_of "$out/$name.tar.gz")  $name.tar.gz" >"$out/$name.tar.gz.sha256"
	rm -rf "$stage"
	echo "listo: $out/$name.tar.gz ($(wc -c <"$out/$name.tar.gz" | tr -d ' ') bytes)"
done
