# Desplegar argos en un Linux (systemd)

Paquete: `argos-server_<version>_linux_<amd64|arm64>.tar.gz` (binario del server + frontend + binarios del agente + esta carpeta).
Se genera en la máquina de desarrollo con `bash scripts/package-server.sh <version>` (salida en `dist/server/`).

## Instalación de un paso
Linux limpio (Debian 12+/Ubuntu 22.04+), acceso root y el paquete descomprimido. UN comando instala Postgres 18, crea la BD con dos roles, instala Caddy con HTTPS, arranca el server y crea el primer admin:
```bash
sudo bash deploy/install-server.sh --public-url https://argos.lan --install-postgres --create-local-db --with-caddy --tls-internal --admin-user admin
```
- **Con dominio público** (Let's Encrypt automático; los puertos 80 y 443 deben llegar al equipo): `--domain argos.midominio.com --install-postgres --create-local-db --with-caddy` (sin `--public-url` ni `--tls-internal`).
- **LAN sin dominio**: `--tls-internal` usa la CA interna de Caddy. Al final el instalador imprime la ruta de la raíz (`/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt`): instálala como autoridad de confianza en los navegadores y pásala a los agentes (`argos-agent install --ca-file root.crt`). `argos.lan` debe resolver (DNS o `/etc/hosts`) en cada equipo.
- **Primer admin**: contraseña aleatoria fuerte, entregada por stdin a `create-admin` e impresa UNA sola vez en un recuadro al final (no se guarda en ningún archivo ni log; anótala). Si ya existe un admin no se crea otro; `--admin-user NOMBRE` cambia el usuario, `--no-admin` lo omite. El instalador usa `create-admin --must-change`: el primer login obliga a cambiar esa contraseña.
- **Revisar antes de ejecutar**: `bash deploy/install-server.sh --dry-run <mismas opciones>` imprime cada paso ([sistema] = toca el sistema) sin ejecutar nada ni exigir root.
- **Idempotente**: re-ejecutar (con o sin opciones) no duplica ni rompe nada; los `.env` y la BD se conservan. Con una BD existente: `--db-url` (y `--migrate-db-url`) en lugar de `--create-local-db`; sin `--install-postgres` exige Postgres 18 ya instalado.
- Solo Debian/Ubuntu automatizan `--install-postgres` y `--with-caddy` (repos oficiales PGDG y Caddy, claves GPG con `signed-by`); en otras distros instala a mano y sigue el flujo manual de abajo. Si ya había un Postgres más viejo en el 5432, el cluster 18 queda en otro puerto y el instalador lo detecta. `ufw` se abre (80/443) solo si está activo; otros firewalls, a mano.

El resto de este documento es el flujo manual y la operación.

## Detrás de un túnel (sin abrir puertos, sin Caddy)
Si el server está en una red casera y lo sacas a Internet con un túnel (p. ej. Cloudflare Tunnel), el túnel termina el HTTPS y reenvía a `127.0.0.1:8080`; **no** uses `--with-caddy`:
```bash
sudo bash deploy/install-server.sh --public-url https://argos.cloud-at-home.net.pe --install-postgres --create-local-db --admin-user admin
```
- `ARGOS_LISTEN_ADDR=127.0.0.1:8080` y `ARGOS_TRUSTED_PROXIES=127.0.0.1/32` ya son los valores por defecto del instalador: el túnel (`cloudflared` u otro) se conecta desde loopback, por eso el server confía en su `X-Forwarded-For` para el límite de intentos y la auditoría. **No agregues otros proxies de confianza.** `ARGOS_PUBLIC_URL` debe ser exactamente el nombre público (con `https://`): alimenta la lista de `Host` permitidos, HSTS y la cookie `__Host-`.
- Configura el túnel para que reenvíe la cabecera `Host` original (es lo normal) hacia `http://127.0.0.1:8080`. Ejemplo de ingreso en `cloudflared`: `hostname: argos.cloud-at-home.net.pe` → `service: http://127.0.0.1:8080`.
- Los agentes se enrolan contra `https://argos.cloud-at-home.net.pe` (el modal "Agregar host" arma el comando con `ARGOS_PUBLIC_URL`). Sin `--insecure`: es HTTPS real.
- El puerto 8080 solo escucha en loopback: no hace falta abrir nada en el router. Verifica desde fuera: `curl -s https://argos.cloud-at-home.net.pe/health`.
- Revisa la revisión de seguridad multi-cliente (`docs/decisiones/`) antes de dar acceso a clientes.

## Requisitos (flujo manual)
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
- **Dos roles de BD** (recomendado). `--create-local-db` crea en el Postgres local `argos_owner` (dueño del esquema: solo migra) y `argos_app` (el que usa el server: DML, sin DDL sobre el resto ni acceso al trigger append-only de `audit_log`), con claves aleatorias. Con otra BD: `--db-url 'postgres://argos_app:…'` y `--migrate-db-url 'postgres://argos_owner:…'` (el dueño debe ser miembro de `argos_app`: `GRANT argos_app TO argos_owner`). Solo `--db-url` = un único rol que migra y opera (funciona, menos seguro).
- La clave del dueño queda en `/etc/argos/argos-migrate.env` (0600 root) y **solo** la ve `argos-admin migrate`; el server en marcha nunca la tiene.
- Crea el usuario de sistema `argos`, deja todo en `/opt/argos`, la config en `/etc/argos/argos.env` (0640 root:argos, con `ARGOS_SECRET_KEY` generada) y activa `argos-server.service` y el respaldo diario.
- **Copia `/etc/argos/argos.env` fuera del server**: sin `ARGOS_SECRET_KEY` los canales de notificación cifrados no se pueden leer.

## 3. HTTPS
Con Caddy: copia `deploy/Caddyfile.example` a `/etc/caddy/Caddyfile` (ajusta el nombre; debe resolver por DNS o `/etc/hosts` en cada equipo y coincidir con `ARGOS_PUBLIC_URL`) y `sudo systemctl reload caddy`. Con `tls internal` Caddy usa su propia CA: instala su raíz (`/var/lib/caddy/.local/share/caddy/pki/authorities/local/root.crt`) en los navegadores y pásala a los agentes con `argos-agent install --ca-file root.crt`. Con un dominio público, quita `tls internal`.
Alternativa sin proxy: `ARGOS_LISTEN_ADDR=0.0.0.0:8443`, `ARGOS_TLS_CERT` y `ARGOS_TLS_KEY` en el env.

## 4. Agentes
Desde el dashboard: Hosts → Agregar host → comando de instalación. El server publica los binarios en `/dl` e `/install.sh`.

## Actualizar
Descomprime el paquete nuevo y corre `sudo bash deploy/install-server.sh` (sin opciones): reemplaza binario, web y agentes, conserva los env y la BD, aplica las migraciones con el rol dueño (`argos-admin migrate`) y reinicia. En modo de un solo rol el server migra solo al arrancar. **Antes de actualizar, haz un respaldo** (`sudo systemctl start argos-backup.service`).

## Respaldo y restauración
- Diario a las 03:15 en `/var/backups/argos/argos-AAAA-MM-DD.dump` (14 días). Prueba: `sudo systemctl start argos-backup.service && ls /var/backups/argos`.
- Restaurar (server parado): `sudo systemctl stop argos-server && pg_restore --clean --if-exists -d "$DATABASE_URL" archivo.dump`.

## Dimensionar (rendimiento)
- Medido con 1000 hosts (`docs/decisiones/2026-09-25-rendimiento.md`): lectura del dashboard 2–60 ms y el ingest aguantó ~300 req/s (≈9000 agentes a 30 s) en un portátil con todo en la misma máquina.
- **RAM:** deja libre ~1 GB por cada 1000 hosts (a 30 s) para las particiones de hoy y ayer con su índice, además de la del server y de Postgres, y sube `shared_buffers` de los 128 MB por defecto a ~25 % de la RAM.
- Una solicitud que tarda ≥ 1 s se registra como Warn en el log: si aparecen seguido en las gráficas de 24 h, falta RAM (o la BD se reinició y la caché está fría).

## Respaldos, baja de un cliente y llaves (runbooks cortos)
- Respaldo diario: `argos-backup.timer` (14 días, en `/var/backups/argos`, sin cifrar; `ARGOS_BACKUP_DIR` y `ARGOS_BACKUP_KEEP_DAYS` en el entorno de la unidad). Copia además `/etc/argos/argos.env` (contiene `ARGOS_SECRET_KEY`) fuera del server: sin esa clave los canales cifrados no se pueden leer y hay que recrearlos.
- Baja de un cliente: exporta antes lo que necesite (informes CSV, historial por fecha), luego "Dar de baja al cliente" en Grupos (o `POST /groups/{id}/purge`). La purga borra métricas, resumen horario, historial comprimido, discos, chequeos y hosts; **no** limpia los respaldos (14 días), ni `audit_log` (conserva ids/nombres 180 días), ni los canales webhook y perfiles SNMP del cliente (bórralos a mano). Los usuarios viewer se deshabilitan, no se borran.
- Reinstalar un agente en el mismo equipo (`enroll --force`) crea un host NUEVO; deshabilita o purga el anterior para no dejar un host fantasma caído.

## Operación
- Salud: `curl -s http://127.0.0.1:8080/health` (200 con BD viva, 503 si Postgres no responde): úsalo en Caddy (`health_uri`) o en un monitor externo. Log de solicitudes: `journalctl -u argos-server | grep solicitud` (los éxitos rutinarios de agentes, `/health` y estáticos van a nivel Debug y no salen).
- Logs: `journalctl -u argos-server -f`. Estado: `systemctl status argos-server`. Auditar el sandbox: `systemd-analyze security argos-server`.
- Migraciones a mano: `sudo argos-admin migrate` (idempotente; también reasigna permisos del rol de runtime).
- Reset de clave de un usuario: `sudo argos-admin reset-password <usuario>`.
- Si una línea de endurecimiento del unit rompe tu distro, comenta SOLO esa.

## Qué NO está validado (2026-09-26)
Se probó: sintaxis de los scripts, el `--dry-run` del instalador y sus funciones puras (validaciones, Caddyfile, env) con mocks en Git Bash, el paquete (contenido, sha256, LF), que el binario con ese layout sirve web y `/dl`, y la separación de roles contra Postgres 18 real (test de integración y prueba de humo con una copia de tu BD real: migrar como dueño, operar como runtime). **No** se ha ejecutado el instalador (ni `--install-postgres`, `--with-caddy` ni la creación del admin) ni el unit en un Linux real ni con Caddy (el desarrollo es en Windows, sin systemd). Primer despliegue: revisa `systemctl status`, `journalctl` y `systemd-analyze security`, y anota cualquier ajuste en `docs/deuda-tecnica.md`.
