# Producción con Docker (Ubuntu)

Cuatro contenedores: **PostgreSQL 18** (datos persistentes, sin puerto público), **argos-server** (API + dashboard + descargas del agente), **Caddy** (HTTPS automático con Let's Encrypt) y **backup** (pg_dump diario). Solo Caddy publica puertos (80 y 443).

## 1. Requisitos
- Servidor Ubuntu 22.04/24.04 con IP pública, y los puertos **80 y 443** abiertos (firewall del proveedor y `ufw allow 80,443/tcp && ufw allow 443/udp`).
- Un dominio propio.

## 2. Instalar Docker
```bash
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER   # cierra sesión y vuelve a entrar
docker compose version          # debe responder (Compose v2)
```

## 3. DNS
Crea un registro **A** `argos.tudominio.com → IP pública del servidor` (y AAAA si usas IPv6). Espera a que propague (`dig +short argos.tudominio.com`). Sin esto, Let's Encrypt no podrá emitir el certificado.

## 4. Clonar y configurar
```bash
git clone <URL-del-repo> argos && cd argos/deploy
cp .env.example .env && chmod 600 .env
openssl rand -hex 24      # pega en POSTGRES_PASSWORD
openssl rand -base64 32   # pega en ARGOS_SECRET_KEY
nano .env                 # DOMAIN, ACME_EMAIL, ARGOS_VERSION
```
**Respalda `deploy/.env` fuera del servidor** (gestor de contraseñas): `ARGOS_SECRET_KEY` cifra los secretos de los canales y los respaldos de la BD no la incluyen.

## 5. Levantar
```bash
docker compose up -d --build     # la primera vez compila web, server y agentes (varios minutos)
docker compose ps                # db y argos-server deben quedar "healthy"
docker compose logs -f argos-server
```
El server aplica las migraciones al arrancar.

## 6. Primer administrador
```bash
docker compose exec argos-server /app/argos-server create-admin admin   # pide la contraseña sin eco
```
(`reset-password <usuario>` funciona igual si la olvidas.)

## 7. Comprobar HTTPS
```bash
curl -I https://argos.tudominio.com/health      # HTTP/2 200
```
Abre `https://argos.tudominio.com` e inicia sesión. Si el certificado falla: `docker compose logs caddy` (casi siempre es DNS sin propagar o puerto 80/443 cerrado). Los certificados viven en el volumen `caddy_data`; no lo borres (Let's Encrypt limita las emisiones).

## 8. Instalar el agente en los equipos
El server publica los agentes de la versión `ARGOS_VERSION`. En el dashboard: **Hosts → Agregar host** genera el comando con el token. Equivalentes:

- **Linux/macOS (script):** `curl --proto '=https' --tlsv1.2 -fsSL https://DOMINIO/install.sh | sudo sh -s -- --server https://DOMINIO --token <TOKEN>`
- **Windows (PowerShell admin):** `https://DOMINIO/install.ps1` (usa el comando que genera el dashboard).
- **Descarga directa:** `https://DOMINIO/dl/latest/manifest.json` lista los archivos con su SHA-256; cada uno está en `https://DOMINIO/dl/<versión>/<archivo>`:
  - `argos-agent_windows_amd64.exe` / `_arm64.exe`
  - `argos-agent_linux_amd64` / `_arm64` (binario)
  - `argos-agent_<versión>_amd64.deb` / `_arm64.deb` (paquete Debian/Ubuntu)
  - `argos-agent_darwin_amd64` / `_arm64`
- **.deb:** `sudo apt install ./argos-agent_<versión>_amd64.deb` solo deja el binario en `/usr/local/bin/argos-agent`; **no enrola**. Luego: `sudo argos-agent install && echo '<TOKEN>' | sudo argos-agent enroll --server https://DOMINIO --token-stdin && sudo argos-agent start`.

Con un binario descargado a mano el orden es siempre `install → enroll → start`.

## 9. Actualizar a una versión nueva
```bash
cd argos && git pull
cd deploy && nano .env          # sube ARGOS_VERSION (ej. 0.1.1)
docker compose up -d --build
```
Los agentes se actualizan en cada equipo descargando la versión nueva (página **Descargar agente** o `/dl/...`) y ejecutando `argos-agent install` como administrador/root: reemplaza el binario y conserva el enrolamiento. **Antes de actualizar**, haz un respaldo manual (sección 10): las migraciones no se revierten. Rollback: vuelve el `git checkout` y `ARGOS_VERSION` anteriores y reconstruye; si la migración ya cambió el esquema, restaura el respaldo.

## 10. Respaldo y restauración
El servicio `backup` escribe `deploy/backups/argos-AAAA-MM-DD.dump` cada día a `BACKUP_HOUR` y borra los de más de `BACKUP_KEEP_DAYS`. Copia esa carpeta fuera del servidor (rsync/rclone).
```bash
# respaldo manual
docker compose exec backup sh -c 'pg_dump -Fc > /backups/argos-manual-$(date +%F).dump'

# restaurar (se pierden los datos actuales)
docker compose stop argos-server
docker compose exec db psql -U argos -d postgres -c 'DROP DATABASE argos WITH (FORCE)' -c 'CREATE DATABASE argos OWNER argos'
docker compose exec -T db pg_restore -U argos -d argos --no-owner < backups/argos-AAAA-MM-DD.dump
docker compose start argos-server
```
Prueba una restauración en otra máquina antes de necesitarla.

## 11. Operación
```bash
docker compose logs -f --tail=100 argos-server   # logs (también: caddy, db, backup)
docker compose ps
docker compose restart argos-server
docker compose down                              # detiene; los volúmenes se conservan
```
**Nunca** uses `docker compose down -v` en producción: borra la base y los certificados.

## Notas
- Caddy es el único proxy de confianza (`ARGOS_TRUSTED_PROXIES=172.28.0.0/24`, subred fija de la red `edge`); de ahí se lee la IP real para el límite de intentos y la auditoría. Si cambias esa subred, cámbiala en las dos partes del compose.
- La base solo está en la red interna `internal` (sin salida a internet ni puerto publicado).
- Más variables opcionales del server (retenciones, webhooks): `server/.env.example`; se añaden en `environment:` de `argos-server`.
