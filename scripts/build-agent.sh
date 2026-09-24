#!/usr/bin/env bash
# Compila el agente pa los 6 objetivos (windows/linux/darwin x amd64/arm64) y genera el manifiesto.
#
# Uso: bash scripts/build-agent.sh <version> [directorio-salida]
#   version           ej. 0.1.0 (letras, digitos, . _ + -)
#   directorio-salida por defecto <repo>/dist/agent  (apuntar ARGOS_AGENT_DIST_DIR aqui)
#
# Salida: <out>/<version>/argos-agent_<os>_<arch>[.exe], <out>/<version>/manifest.json y <out>/latest
# (archivo de texto con la version). SIN firmas: el SHA-256 del manifiesto solo protege la descarga; no hay
# auto-actualizacion hasta tener firmas (ver docs/decisiones).
set -euo pipefail

version="${1:-}"
if [[ ! "$version" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]]; then
	echo "uso: $0 <version> [directorio-salida]  (version invalida o vacia)" >&2
	exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="${2:-$root/dist/agent}"
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
for t in "${targets[@]}"; do
	goos="${t%/*}"
	goarch="${t#*/}"
	name="argos-agent_${goos}_${goarch}"
	[[ "$goos" == windows ]] && name="$name.exe"
	echo "compilando $name"
	(cd "$root/agent" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
		go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$dir/$name" ./cmd/agent)
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
