# Desplegar argos en un Linux (systemd)

Paquete: `argos-server_<version>_linux_<amd64|arm64>.tar.gz` (binario del server + frontend + binarios del agente + esta carpeta).
Se genera en la máquina de desarrollo con `bash scripts/package-server.sh <version>` (salida en `dist/server/`).

## Requisitos
- Linux con systemd (probado el diseño para Ubuntu 22.04+/Debian 12+), acceso root.
- **PostgreSQL 18** (el server lo exige) y, para el respaldo, `postgresql-client-18`.
- Para HTTPS: Caddy (o nginx). La cookie de sesión lleva el prefijo `__Host-` y exige HTTPS: por http en LAN el login solo funciona desde `localhost`.

## 1. Postgres 18
Opción A, paquete oficial PGDG (Ubuntu/Debian):
```bash
sudo apt install -y curl ca-certificates
sudo install -d /usr/share/postgresql-common/pgdg
sudo curl -fo /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc
. /etc/os-release
echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt ${VERSION_CODENAME}-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
sudo apt update && sudo apt install -y postgresql-18 postgresql-client-18
```
Opción B, Docker: usa `docker-compose.yml` de la raíz del repo, cambiando `POSTGRES_PASSWORD`, y pásale a la instalación `--db-url`.

## 2. Instalar
```bash
tar -xzf argos-server_<version>_linux_amd64.tar.gz && cd argos-server_<version>_linux_amd64
sha256sum -c ../argos-server_<version>_linux_amd64.tar.gz.sha256      # si copiaste el .sha256
sudo bash deploy/install-server.sh --public-url https://argos.lan --create-local-db
sudo argos-admin create-admin admin
```
- `--create-local-db` crea rol y BD `argos` en el Postgres local con clave aleatoria; con otra BD usa `--db-url 'postgres://usuario:clave@host:5432/argos?sslmode=...'`.
- Crea el usuario de sistema `argos`, deja todo en `/opt/argos`, la config en `/etc/argos/argos.env` (0640 root:argos, con `ARGOS_SECRET_KEY` generada) y activa `argos-server.service` y el respaldo diario.
- **Copia `/etc/argos/argos.env` fuera del server**: sin `ARGOS_SECRET_KEY` los canales de notificación cifrados no se pueden leer.

## 3. HTTPS
Con Caddy: copia `deploy/Caddyfile.example` a `/etc/caddy/Caddyfile` (ajusta el nombre; debe resolver por DNS o `/etc/hosts` en cada equipo y coincidir con `ARGOS_PUBLIC_URL`) y `sudo systemctl reload caddy`. Con `tls internal` Caddy usa su propia CA: instala su raíz (`/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt`) en los navegadores y pásala a los agentes con `argos-agent install --ca-file root.crt`. Con un dominio público, quita `tls internal`.
Alternativa sin proxy: `ARGOS_LISTEN_ADDR=0.0.0.0:8443`, `ARGOS_TLS_CERT` y `ARGOS_TLS_KEY` en el env.

## 4. Agentes
Desde el dashboard: Hosts → Agregar host → comando de instalación. El server publica los binarios en `/dl` e `/install.sh`.

## Actualizar
Descomprime el paquete nuevo y corre `sudo bash deploy/install-server.sh` (sin opciones): reemplaza binario, web y agentes, conserva el env y la BD, y reinicia. Las migraciones se aplican solas al arrancar. **Antes de actualizar, haz un respaldo** (`sudo systemctl start argos-backup.service`).

## Respaldo y restauración
- Diario a las 03:15 en `/var/backups/argos/argos-AAAA-MM-DD.dump` (14 días). Prueba: `sudo systemctl start argos-backup.service && ls /var/backups/argos`.
- Restaurar (server parado): `sudo systemctl stop argos-server && pg_restore --clean --if-exists -d "$DATABASE_URL" archivo.dump`.

## Operación
- Logs: `journalctl -u argos-server -f`. Estado: `systemctl status argos-server`. Auditar el sandbox: `systemd-analyze security argos-server`.
- Reset de clave de un usuario: `sudo argos-admin reset-password <usuario>`.
- Si una línea de endurecimiento del unit rompe tu distro, comenta SOLO esa.

## Qué NO está validado (2026-09-25)
Se probó: sintaxis de los scripts, el paquete (contenido, sha256, LF), y que el binario con ese layout sirve web y `/dl`. **No** se ha ejecutado el instalador ni el unit en un Linux real ni con Caddy (el desarrollo es en Windows, sin systemd). Primer despliegue: revisa `systemctl status`, `journalctl` y `systemd-analyze security`, y anota cualquier ajuste en `docs/deuda-tecnica.md`.
