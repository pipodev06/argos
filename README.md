# argos

Monitoreo de hosts, dispositivos y servicios (tipo Zabbix/PRTG), pensado para hospedarse como servicio para varios clientes.

- **Agente** (`agent/`, Go): métricas, inventario, parches, chequeos de servicio y colector de ping/SNMP. Windows, Linux (systemd) y macOS.
- **Servidor** (`server/`, Go + PostgreSQL 18): API, ingesta, detección de caídas, alertas con webhooks (Slack, Discord, receptor propio), informes y retención por cliente.
- **Dashboard** (`web/`, React + Vite + Tailwind v4 + shadcn/ui): servido por el propio servidor.

Documentación interna: `docs/estado-del-proyecto.md` (resumen), `docs/decisiones.md` (índice de decisiones) y `deploy/README.md` (despliegue completo).

## Desarrollo

### Requisitos
- Go 1.27.2 o superior, Node 22 o superior y Git Bash (en Windows).
- PostgreSQL 18 local con usuario y clave `argos` y dos bases de datos: `argos` (la de uso diario) y `argos_test` (solo para tests). Si no tienes Postgres instalado, `docker compose up -d` levanta uno igual en `127.0.0.1:5432`.
- La clave de cifrado del servidor, creada una sola vez:
  ```bash
  mkdir -p ~/.argos-dev && openssl rand -hex 32 > ~/.argos-dev/secret-key.txt
  ```

### Levantar en local
```bash
bash scripts/run-local.sh start --web   # compila web + server y arranca en segundo plano
bash scripts/run-local.sh status        # estado y /health
bash scripts/run-local.sh logs          # log en vivo
bash scripts/run-local.sh stop
```
Abre el dashboard en `http://localhost:8080`. Usa `localhost`, no la IP: la cookie de sesión `__Host-` exige HTTPS salvo en localhost. El servidor aplica las migraciones al arrancar, así que respalda la base `argos` antes de probar una migración nueva.

Primer usuario administrador:
```bash
cd server && DATABASE_URL='postgres://argos:argos@localhost:5432/argos?sslmode=disable' \
  ARGOS_SECRET_KEY="$(cat ~/.argos-dev/secret-key.txt)" go run ./cmd/server create-admin admin   # pide la contraseña sin eco
```

Para editar la interfaz con recarga en caliente, deja el servidor corriendo y usa `cd web && npm run dev` (Vite en `http://localhost:5173`, con `/api` redirigido a `:8080`).

### Agente local
```bash
bash scripts/build-agent.sh 0.0.0-dev            # binarios en dist/agent (sin firma: sirve para pruebas, no permite `update`)
```
Enrólalo desde el dashboard: **Hosts → Agregar host** genera el comando. Los equipos de la LAN que hablen por `http://` necesitan `--insecure` (solo en desarrollo).

### Verificar antes de commitear
```bash
export ARGOS_TEST_DATABASE_URL='postgres://argos:argos@localhost:5432/argos_test?sslmode=disable'
bash scripts/check-go.sh all     # vet + tests de server y agente (--race requiere gcc)
bash scripts/check-web.sh        # tipos, lint, tests y build del frontend
```
Si no defines `ARGOS_TEST_DATABASE_URL`, los tests de base de datos se saltan sin avisar y el resultado sale en verde igual.

## Producción

Resumen; el detalle (túnel, HTTPS, roles de BD, respaldos, firma de binarios) está en `deploy/README.md`.

1. **Empaquetar** en la máquina de desarrollo:
   ```bash
   bash scripts/build-agent.sh <version> --key ~/.argos-dev/agent-sign.key   # agentes firmados
   bash scripts/package-server.sh <version> --build-web                       # dist/server/*.tar.gz
   ```
2. **Instalar** en un Debian 12+/Ubuntu 22.04+ limpio, con el paquete descomprimido:
   ```bash
   # detrás de un túnel (Cloudflare u otro), sin abrir puertos:
   sudo bash deploy/install-server.sh --public-url https://argos.midominio.com --install-postgres --create-local-db --admin-user admin
   # o con Caddy y HTTPS propio:
   sudo bash deploy/install-server.sh --domain argos.midominio.com --install-postgres --create-local-db --with-caddy --admin-user admin
   ```
   Agrega `--dry-run` para ver los pasos sin ejecutar nada. La contraseña del admin se imprime una sola vez al final.
3. **Guarda fuera del servidor** `/etc/argos/argos.env`: sin `ARGOS_SECRET_KEY` no se pueden leer los canales cifrados.
4. **Actualizar**: primero respalda (`sudo systemctl start argos-backup.service`) y después corre `sudo bash deploy/install-server.sh` con el paquete nuevo.
5. **Monitorear el propio servicio**: `GET /health` (sin auth) y `GET /metrics` (formato Prometheus, con `ARGOS_METRICS_TOKEN`). Dentro del dashboard, el admin lo ve en Configuración → Estado del servicio.

Antes de recibir el primer cliente falta: protección del login desde Internet (bloqueo de cuenta o Cloudflare Access) y respaldos cifrados fuera del host. Ver `docs/plan-tareas.md`.
