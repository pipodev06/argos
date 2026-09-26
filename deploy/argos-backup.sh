#!/usr/bin/env bash
# Respaldo diario de la BD de argos (pg_dump formato custom) con retencion. Lo ejecuta argos-backup.timer.
# Requiere postgresql-client-18 (pg_dump >= version del server). NO respalda la clave: copia aparte
# /etc/argos/argos.env (contiene ARGOS_SECRET_KEY, sin ella los canales cifrados no se pueden leer).
set -euo pipefail
: "${DATABASE_URL:?falta DATABASE_URL (lo carga systemd desde /etc/argos/argos.env)}"
dir="${ARGOS_BACKUP_DIR:-/var/backups/argos}"
keep_days="${ARGOS_BACKUP_KEEP_DAYS:-14}"
mkdir -p "$dir"
tmp="$dir/.argos-$(date +%F).dump.partial"
trap 'rm -f "$tmp"' EXIT
pg_dump --format=custom --file="$tmp" "$DATABASE_URL"
mv "$tmp" "$dir/argos-$(date +%F).dump"
find "$dir" -maxdepth 1 -name 'argos-*.dump' -mtime +"$keep_days" -delete
echo "respaldo listo: $dir/argos-$(date +%F).dump"
