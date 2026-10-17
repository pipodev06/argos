# Operacion, tooling y variables de entorno
Variables verificadas contra `server/internal/config/config.go`, `agent/internal/config/config.go` y los `.env.example` (2026-09-24). Sin contradicciones entre docs y codigo.

## Server (`DATABASE_URL` obligatoria; el resto opcional)
| Variable | Default / limite | Detalle en |
|---|---|---|
| `ARGOS_LISTEN_ADDR` | `127.0.0.1:8080` | api-servidor |
| `ARGOS_PUBLIC_URL` | vacio; https activa HSTS | auth-y-sesiones, csp-y-cabeceras |
| `ARGOS_TRUSTED_PROXIES` | vacio; lista IP/CIDR | auth-y-sesiones |
| `ARGOS_TLS_CERT` / `ARGOS_TLS_KEY` | ambas o ninguna; TLS >= 1.2 | auth-y-sesiones |
| `ARGOS_INSECURE_COOKIES` | false (solo dev) | auth-y-sesiones |
| `ARGOS_AUDIT_RETENTION_DAYS` | 180, min 30 | auth-y-sesiones |
| `ARGOS_HOST_DOWN_AFTER` / `ARGOS_HOST_CHECK_INTERVAL` | 2m (min 30s) / 15s (min 1s, <= DOWN_AFTER) | estados-y-deteccion |
| `ARGOS_METRICS_RETENTION_DAYS` | 30, min 8 (default global del DETALLE comprimido; editable en la pantalla Configuración) | detalle-comprimido-retencion |
| `ARGOS_METRICS_HOT_DAYS` | 8, min 8 (ventana del crudo antes de empaquetar) | detalle-comprimido-retencion |
| `ARGOS_AUTO_MIGRATE` | true (false = el server no migra; usa `argos-server migrate`) | roles-bd |
| `ARGOS_MIGRATE_DATABASE_URL`, `ARGOS_APP_ROLE` | solo para `argos-server migrate` (rol dueño y rol de runtime) | roles-bd |
| `ARGOS_METRICS_HOURLY_RETENTION_DAYS` | 400, min 8, >= cruda | datos-retencion-particionado |
| `ARGOS_SERVICE_CHECKS_RETENTION_DAYS` | 14, min 8 | datos-retencion-particionado |
| `ARGOS_DISK_RETENTION_DAYS` | 30, min 8 | grupos-etiquetas-metricas |
| `ARGOS_METRICS_MAINTENANCE_INTERVAL` | 5m, min 5s | datos-retencion-particionado |
| `ARGOS_TEST_DATABASE_URL` | solo tests; BD debe terminar en `_test` | api-servidor |

## Agente
| Variable | Default / limite | Detalle en |
|---|---|---|
| `ARGOS_SERVER_URL` | URL base (sin path) | agente-transporte-y-estado |
| `ARGOS_AGENT_TOKEN` | override del token del archivo | agente-transporte-y-estado |
| `ARGOS_ENROLL_TOKEN` | alternativa a `--token`/`--token-stdin` | agente-colectores-y-servicio |
| `ARGOS_STATE_DIR` | `os.UserConfigDir()/argos-agent` (o dir del servicio) | agente-transporte-y-estado |
| `ARGOS_CA_FILE` | opcional; si no, `<StateDir>/ca.pem` | agente-colectores-y-servicio |
| `ARGOS_INTERVAL` | 30s, min 5s | agente-transporte-y-estado |
| `ARGOS_DISK_INTERVAL` | 300s, min 60s | agente-colectores-y-servicio |
| `ARGOS_ALLOW_INSECURE` | `1\|true\|0\|false`; http fuera de loopback | agente-transporte-y-estado |

`install` no pasa estas variables al servicio del SO (ver deuda).

## Tooling y flujo
- Scripts: `scripts/check-go.sh`, `scripts/check-web.sh`. `.golangci.yml` v2 por modulo (ver api-servidor). Web: `build` y `lint` (sin tests).
- Tests con Postgres real: `ARGOS_TEST_DATABASE_URL=postgres://.../argos_test?sslmode=disable go test ./...` (sin la variable se saltan; paquetes serializados con lock advisory sondeado).
- `go test -race` en Windows necesita gcc (CGO): correr en CI Linux o instalar MSYS2/mingw (`devops-engineer`). Hoy no hay CI ni Dockerfile.
- E2E manual: server propio en 127.0.0.1:18080 contra `argos_test` (o Postgres temporal con initdb en scratchpad, puerto 55432); NO tocar la BD `argos` ni el server/Vite de :8080/:5173. Limpiar todo al terminar.
- `docker-compose.yml`: solo `db` = `postgres:18`, user/pass `argos`/`argos` (igual que la BD local de desarrollo; en producción cambiar `POSTGRES_PASSWORD`), puerto `127.0.0.1:5432`, volumen en `/var/lib/postgresql` (PG18+).
- BD local `argos`: ultimo dato documentado = 00004 aplicada (con backup; `srv-test-01` intacto, no borrarlo); 00006-00013 pendientes de backup + reinicio del server (ver estado). Sin usuarios en `argos`: crear el admin con `argos-server create-admin`.
