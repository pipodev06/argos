#!/bin/sh
# Respaldo diario de la BD de argos (pg_dump formato custom) con retencion. Corre dentro del servicio "backup"
# (imagen postgres:18). NO respalda la clave ARGOS_SECRET_KEY: copia aparte deploy/.env.
# Variables: PGHOST PGUSER PGPASSWORD PGDATABASE (las pone el compose), BACKUP_HOUR (0-23, def. 3), BACKUP_KEEP_DAYS (def. 14).
set -eu
umask 077
dir=/backups
hour="${BACKUP_HOUR:-3}"
keep="${BACKUP_KEEP_DAYS:-14}"

backup() {
	f="$dir/argos-$(date +%F).dump"
	tmp="$dir/.argos.partial"
	if pg_dump --format=custom --file="$tmp" && mv "$tmp" "$f"; then
		find "$dir" -maxdepth 1 -name 'argos-*.dump' -mtime +"$keep" -delete
		echo "respaldo listo: $f"
	else
		rm -f "$tmp"
		echo "ERROR: fallo el respaldo" >&2
	fi
}

while true; do
	now=$(date +%s)
	next=$(date -d "today ${hour}:00" +%s)
	[ "$next" -gt "$now" ] || next=$(date -d "tomorrow ${hour}:00" +%s)
	echo "proximo respaldo: $(date -d "@$next" '+%F %T')"
	sleep $((next - now))
	backup
done
