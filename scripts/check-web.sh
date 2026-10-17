#!/usr/bin/env bash
# Verificacion del frontend con salida MINIMA (el log completo va a .check/).
# Uso: scripts/check-web.sh
set -u
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
mkdir -p "$ROOT/.check"
cd "$ROOT/web" || exit 2
FAILED=0
for name in build lint test; do
  log="$ROOT/.check/web-$name.log"; t0=$SECONDS
  if pnpm run "$name" >"$log" 2>&1; then
    warns="$(grep -ci 'warning' "$log")"
    echo "OK   web/$name ($((SECONDS - t0))s, $warns lineas con 'warning')"
  else
    FAILED=1
    echo "FAIL web/$name ($((SECONDS - t0))s) -> log completo: .check/web-$name.log"
    grep -E 'error|Error|TS[0-9]+' "$log" | head -25
  fi
done
[ "$FAILED" -eq 0 ] && echo "RESULTADO: OK" || echo "RESULTADO: FALLO"
exit "$FAILED"
