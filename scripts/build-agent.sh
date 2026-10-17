#!/usr/bin/env bash
# Compila el agente pa los 6 objetivos (windows/linux/darwin x amd64/arm64) y genera el manifiesto.
#
# Uso: bash scripts/build-agent.sh <version> [directorio-salida]
#   version           ej. 0.1.0 (letras, digitos, . _ + -)
#   directorio-salida por defecto <repo>/dist/agent  (apuntar ARGOS_AGENT_DIST_DIR aqui)
# Salida: <out>/<version>/argos-agent_<os>_<arch>[.exe], <out>/<version>/manifest.json (sha256 y tamaño de
# cada binario; lo usa el instalador) y <out>/latest (archivo de texto con la version).
set -euo pipefail

version="${1:-}"
if [[ ! "$version" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]]; then
	echo "uso: $0 <version> [directorio-salida] (version invalida o vacia)" >&2
	exit 2
fi
shift

out="${1:-}"
[[ $# -le 1 ]] || { echo "argumento de mas: $2" >&2; exit 2; }

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${out:-$root/dist/agent}"
# Absoluto: el build hace "cd" a agent/ antes de compilar, y un $out relativo se resolveria mal desde ahi.
mkdir -p "$out"
out="$(cd "$out" && pwd)"
dir="$out/$version"

if [[ -e "$dir" ]]; then
	echo "ya existe $dir: las versiones publicadas son inmutables, usa otra version" >&2
	exit 1
fi

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d ' ' -f 1; else shasum -a 256 "$1" | cut -d ' ' -f 1; fi
}
size_of() { wc -c <"$1" | tr -d ' '; }

mkdir -p "$dir"
targets=(windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64)
entries=()
ldflags="-s -w -X main.version=$version"
for t in "${targets[@]}"; do
	goos="${t%/*}"
	goarch="${t#*/}"
	name="argos-agent_${goos}_${goarch}"
	[[ "$goos" == windows ]] && name="$name.exe"
	echo "compilando $name"
	(cd "$root/agent" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags "$ldflags" -o "$dir/$name" ./cmd/agent)
	entries+=("{\"name\":\"$name\",\"os\":\"$goos\",\"arch\":\"$goarch\",\"sha256\":\"$(sha256_of "$dir/$name")\",\"size\":$(size_of "$dir/$name")}")
done

{
	printf '{\n"version":"%s",\n"files":[\n' "$version"
	last=$((${#entries[@]} - 1))
	for i in "${!entries[@]}"; do
		if [[ "$i" -lt "$last" ]]; then printf '%s,\n' "${entries[$i]}"; else printf '%s\n' "${entries[$i]}"; fi
	done
	printf ']\n}\n'
} >"$dir/manifest.json"

# latest se actualiza al final y de forma atomica: el server nunca ve una version a medio generar.
printf '%s\n' "$version" >"$out/latest.tmp"
mv -f "$out/latest.tmp" "$out/latest"
echo "listo: $dir (latest=$version)"
