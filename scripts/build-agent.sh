#!/usr/bin/env bash
# Compila el agente pa los 6 objetivos (windows/linux/darwin x amd64/arm64) y genera el manifiesto.
#
# Uso: bash scripts/build-agent.sh <version> [directorio-salida] [--key <archivo-clave-privada>]
#   version           ej. 0.1.0 (letras, digitos, . _ + -)
#   directorio-salida por defecto <repo>/dist/agent  (apuntar ARGOS_AGENT_DIST_DIR aqui)
#   --key             archivo de clave privada ed25519 de `argos-server keygen` (H7). Con esta opcion el
#                      agente se compila con la clave PUBLICA embebida (-ldflags -X main.publicKey=...) y
#                      el manifiesto queda firmado (manifest.json.sig) junto a el. SIN --key el agente
#                      compila igual pero sin clave publica: `update` fallara cerrado en ese binario
#                      (agent/internal/updater: ErrNoPublicKey), pensado solo para desarrollo local.
#                      La clave privada NUNCA se copia al server desplegado ni se sube al repo.
#
# Salida: <out>/<version>/argos-agent_<os>_<arch>[.exe], <out>/<version>/manifest.json (+ .sig si hay
# --key) y <out>/latest (archivo de texto con la version).
set -euo pipefail

version="${1:-}"
if [[ ! "$version" =~ ^[0-9A-Za-z][0-9A-Za-z._+-]{0,63}$ ]]; then
	echo "uso: $0 <version> [directorio-salida] [--key <archivo-clave-privada>]  (version invalida o vacia)" >&2
	exit 2
fi
shift

out=""
keyfile=""
while [[ $# -gt 0 ]]; do
	case "$1" in
	--key)
		keyfile="${2:-}"
		[[ -n "$keyfile" ]] || { echo "--key necesita un archivo" >&2; exit 2; }
		shift 2
		;;
	*)
		[[ -z "$out" ]] || { echo "argumento de mas: $1" >&2; exit 2; }
		out="$1"
		shift
		;;
	esac
done

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

pubkey=""
if [[ -n "$keyfile" ]]; then
	[[ -f "$keyfile" ]] || { echo "no existe la clave privada: $keyfile" >&2; exit 1; }
	keyfile="$(cd "$(dirname "$keyfile")" && pwd)/$(basename "$keyfile")" # absoluto: los sub-go run hacen cd antes
	pubkey="$(cd "$root/server" && go run ./cmd/server pubkey "$keyfile")"
	echo "clave publica (para --key en futuros builds): $pubkey"
else
	echo "aviso: sin --key, el agente compila sin clave publica embebida; update fallara cerrado (solo para desarrollo local)" >&2
fi

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d ' ' -f 1; else shasum -a 256 "$1" | cut -d ' ' -f 1; fi
}
size_of() { wc -c <"$1" | tr -d ' '; }

mkdir -p "$dir"
targets=(windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64)
entries=()
ldflags="-s -w -X main.version=$version"
[[ -n "$pubkey" ]] && ldflags="$ldflags -X main.publicKey=$pubkey"
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

if [[ -n "$keyfile" ]]; then
	(cd "$root/server" && go run ./cmd/server sign-manifest "$dir/manifest.json" "$keyfile")
fi

# latest se actualiza al final y de forma atomica: el server nunca ve una version a medio generar.
printf '%s\n' "$version" >"$out/latest.tmp"
mv -f "$out/latest.tmp" "$out/latest"
echo "listo: $dir (latest=$version)"
