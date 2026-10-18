<h1><img src="docs/assets/logo.svg" alt="argos" height="40"></h1>

Monitoreo de hosts, dispositivos y servicios (tipo Zabbix/PRTG), pensado para hospedarse como servicio para varios clientes.

- **Agente** (`agent/`, Go): métricas, inventario, parches, chequeos de servicio y colector de ping/SNMP. Windows, Linux (systemd) y macOS.
- **Servidor** (`server/`, Go + PostgreSQL 18): API, ingesta, detección de caídas, alertas con webhooks (Slack, Discord, receptor propio), informes y retención por cliente.
- **Dashboard** (`web/`, React + Vite + Tailwind v4 + shadcn/ui): servido por el propio servidor.

Documentación interna: `docs/estado-del-proyecto.md` (resumen) y `docs/decisiones.md` (índice de decisiones). La instalación en producción está en `deploy/`.

## Producción

Docker Compose con PostgreSQL 18, el servidor, HTTPS automático (Caddy + Let's Encrypt), respaldo diario y descarga de agentes (Windows, Linux `.deb`/binario, macOS). Guía paso a paso para Ubuntu: [`deploy/README.md`](deploy/README.md).

## Desarrollo

Dos piezas, dos herramientas:
- **Server** (Go): se compila a un ejecutable (`.dev-bin/argos-server.exe`) que sirve la API **y** la web ya compilada en `:8080`. `run-local.sh` compila y arranca todo; no hay backend en Node.
- **Web** (Node): solo hace falta `pnpm dev` si vas a editar pantallas y quieres recarga en caliente (`:5173`). Usa el mismo server de `:8080` por detrás.

### Requisitos
- Go 1.27.2 o superior, Node 22 o superior con pnpm (`corepack enable` lo activa) y Git Bash (en Windows).
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

Para editar la interfaz con recarga en caliente, deja el servidor corriendo y usa `cd web && pnpm dev` (Vite en `http://localhost:5173`, con `/api` redirigido a `:8080`).

### Agente local
```bash
bash scripts/build-agent.sh 0.0.0-dev            # binarios + manifest.json en dist/agent
```
Enrólalo desde el dashboard: **Hosts → Agregar host** genera el comando. Los equipos de la LAN que hablen por `http://` necesitan `--insecure` (solo en desarrollo).

A mano en este equipo (PowerShell como administrador, desde la raíz). El orden es **install → enroll → start**:
```powershell
$a = '.\dist\agent\' + (Get-Content .\dist\agent\latest) + '\argos-agent_windows_amd64.exe'
& $a install
'<token>' | & $a enroll --server http://localhost:8080 --token-stdin
& $a start
```
Para actualizar un agente ya instalado basta `& $a install` con la versión nueva: detiene, reemplaza el binario y vuelve a arrancar, conservando el enrolamiento.

#### Opciones del agente
Se pasan a `install` y quedan en `settings.json` del estado. Sobre un agente ya instalado, `install` con opciones las aplica y reinicia el servicio, sin enrolar de nuevo:
```powershell
& $a install --inventory all                     # puertos, servicios, procesos y parches (o una lista: ports,services,processes,updates)
& $a install --probe-allow 192.168.100.0/24      # puede sondear (ping/SNMP) dispositivos de esas redes (CIDR, /24 o más chicas, separadas por comas)
& $a install --insecure                          # acepta un servidor http fuera de localhost (solo LAN de desarrollo)
& $a install --inventory all --probe-allow 192.168.100.0/24   # se combinan; un flag vacío lo apaga: --probe-allow ""
argos-agent status                               # servicio, enrolamiento y opciones activas (desde cualquier consola nueva)
```
Modo privilegiado (ve procesos, puertos y servicios de otros usuarios; en Windows el servicio corre como LocalSystem) y `--ca-file` cambian la definición del servicio: hay que reinstalar conservando el enrolamiento (sin `--keep-state` se borran las credenciales):
```powershell
& $a stop
& $a uninstall --keep-state
& $a install --privileged                        # para volver al modo normal: lo mismo sin --privileged
& $a start
```
Tras instalar, `argos-agent` queda en el PATH: en Windows se agrega la carpeta del binario al PATH de máquina (funciona en consolas **nuevas**; en la actual usa `& $a …`); en Linux/macOS se crea el enlace `/usr/local/bin/argos-agent`. `uninstall` lo quita.

Un dispositivo (ping/SNMP) pasa de "Pendiente" a activo cuando hay un chequeo **Ping** que lo alcanza, su colector tiene `--probe-allow` para su red y ambos están en el mismo grupo.

En Linux/macOS son los mismos subcomandos con `sudo ./argos-agent …`.

Olvidaste la contraseña: `cd server && go run ./cmd/server reset-password <usuario>` (con las mismas variables que `create-admin`).

### Verificar antes de commitear
```bash
export ARGOS_TEST_DATABASE_URL='postgres://argos:argos@localhost:5432/argos_test?sslmode=disable'
bash scripts/check-go.sh all     # vet + tests de server y agente (--race requiere gcc)
bash scripts/check-web.sh        # tipos, lint, tests y build del frontend
```
Si no defines `ARGOS_TEST_DATABASE_URL`, los tests de base de datos se saltan sin avisar y el resultado sale en verde igual.
