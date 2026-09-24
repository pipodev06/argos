#!/usr/bin/env bash
# Verificacion de un modulo Go con salida MINIMA (ahorra tokens: el log completo va a .check/).
# Uso: scripts/check-go.sh <server|agent|all> [--fast] [--pkg ./ruta]
#   --fast   omite golangci-lint y govulncheck (bucle de desarrollo)
#   --pkg    acota tests (y vet) a un paquete, p. ej. --pkg ./internal/store
# Tests de BD: define ARGOS_TEST_DATABASE_URL (BD que termine en _test).
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export PATH="$PATH:$HOME/go/bin"
FAST=0; PKG="./..."; MODS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --fast) FAST=1 ;;
    --pkg) shift; PKG="$1" ;;
    server|agent) MODS+=("$1") ;;
    all) MODS=(server agent) ;;
    *) echo "uso: $0 <server|agent|all> [--fast] [--pkg ./ruta]"; exit 2 ;;
  esac
  shift
done
[ ${#MODS[@]} -gt 0 ] || { echo "uso: $0 <server|agent|all> [--fast] [--pkg ./ruta]"; exit 2; }
mkdir -p "$ROOT/.check"
FAILED=0

step() { # step <modulo> <nombre> <max_lineas_si_falla> -- comando...
  local mod="$1" name="$2" max="$3"; shift 4
  local log="$ROOT/.check/$mod-$name.log" t0=$SECONDS
  if "$@" >"$log" 2>&1; then
    echo "OK   $mod/$name ($((SECONDS - t0))s)"
  else
    FAILED=1
    echo "FAIL $mod/$name ($((SECONDS - t0))s) -> log completo: .check/$mod-$name.log"
    grep -v '^\s*$' "$log" | head -n "$max"
  fi
}

for mod in "${MODS[@]}"; do
  cd "$ROOT/$mod" || exit 2
  # gofmt: falla si lista archivos
  fmt_out="$(gofmt -l . 2>&1)"
  if [ -z "$fmt_out" ]; then echo "OK   $mod/gofmt"; else FAILED=1; echo "FAIL $mod/gofmt (sin formatear):"; echo "$fmt_out" | head -10; fi
  step "$mod" vet 15 -- go vet "$PKG"
  if [ "$FAST" -eq 0 ]; then
    step "$mod" lint 20 -- golangci-lint run
  fi
  # tests: solo lineas FAIL/panic/--- FAIL y el resumen; el detalle queda en el log
  log="$ROOT/.check/$mod-test.log"; t0=$SECONDS
  if go test -count=1 "$PKG" >"$log" 2>&1; then
    echo "OK   $mod/test ($((SECONDS - t0))s, $(grep -c '^ok' "$log") paquetes ok)"
  else
    FAILED=1
    echo "FAIL $mod/test ($((SECONDS - t0))s) -> log completo: .check/$mod-test.log"
    grep -E '^(--- FAIL|FAIL|panic:|.*_test\.go:[0-9]+:)' "$log" | head -25
  fi
  if [ "$FAST" -eq 0 ]; then
    step "$mod" vuln 12 -- govulncheck ./...
  fi
done
[ "$FAILED" -eq 0 ] && echo "RESULTADO: OK" || echo "RESULTADO: FALLO"
exit "$FAILED"
