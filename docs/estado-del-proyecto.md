# Estado del proyecto (actualizado 2026-09-24; una linea por hito; detalle en `decisiones/`, historia en git)
Leyenda: HECHO = implementado y probado con tests/e2e de API; PARCIAL; NO EMPEZADO. "Sin probar" = sin navegador o sin integracion real.

## Server (`server/`, Go, Postgres, goose 00001-00013)
- HECHO Base: config por env, slog, limites, apagado ordenado, migraciones goose embebidas 00001-00013 (probadas Up/Down con datos) -> `decisiones/api-servidor.md`
- HECHO Auth de dashboard: usuarios admin/viewer, sesion en cookie, Argon2id, bloqueo, rate limits, CSRF, auditoria, CLI `create-admin`/`reset-password`, TLS opcional -> `decisiones/auth-y-sesiones.md`
- HECHO Enroll con token hasheado (TTL 15 min, max 50 activos, `group_id`), revocacion de hosts
- HECHO Ingest de metricas (swap, load, bateria, discos, `host_info`, `host_latest`) y series (1h..1y) con huecos/reinicios
- HECHO Detector de caidos (`down`/`offline`, umbral por grupo) -> `decisiones/estados-y-deteccion.md`
- HECHO API de grupos, etiquetas, bulk, `GET /summary`, `GET /hosts` con filtros/orden/cursor/`total` -> `decisiones/grupos-etiquetas-metricas.md`
- HECHO Particionado diario, resumen por hora, retencion (metrics, service_checks, disk_samples) -> `decisiones/datos-retencion-particionado.md`
- PARCIAL La BD real `argos` solo tiene 00004 documentada; 00006-00013 sin migrar hasta backup + reinicio del server; sin usuarios (crear admin)
- NO EMPEZADO Motor de alertas/notificaciones, tabla `host_events`, MFA, rotacion del token de agente, servir el SPA desde el binario (fallback `index.html`), servir instaladores del agente
- Sin verificar: `go test -race` (sin gcc), volumen (1000 hosts) de lista/resumen, integracion con `web/` y agente reales, SIGTERM real

## Agente (`agent/`, Go, gopsutil v4)
- HECHO Metricas reales (CPU, memoria, red, swap, load, bateria, discos por montaje, `host_info`), enroll, estado en disco, transporte seguro, backoff + `Retry-After` + buffer en memoria (120) -> `decisiones/agente-transporte-y-estado.md`
- HECHO Servicio nativo `install|uninstall|start|stop|status` (Windows cuenta virtual, systemd endurecido, LaunchDaemon) -> `decisiones/agente-colectores-y-servicio.md`
- Sin verificar: instalacion real en Windows/Linux/macOS, `systemd-analyze security`, macOS (ni ejecucion), registro de eventos de Windows, SmartScreen
- NO EMPEZADO Buffer offline en disco (libreria por decidir), firma de binarios, auto-actualizacion, pasar config de entorno al servicio desde `install`

## Web (`web/`, Vite + React + Tailwind v4 + shadcn, estilo Slate)
- HECHO Login/sesion, cambio de clave normal y forzado, 401 central, tema claro/oscuro por boton, `UserMenu`
- HECHO Shell (`AppShell` + sidebar 240/56 px + drawer movil), react-router 8.4.0 (`/` -> `/hosts`, `/hosts`, `/hosts/:id`, `*`), componentes compartidos (`PageHeader`, `EmptyState`, `Pagination`, `FilterChip`, `UsageBar`, `StatusTile`, `CopyBlock`, 12 shadcn en Slate) -> `decisiones/frontend-estilo-y-componentes.md`
- HECHO Lista de hosts simple (polling 10 s) y "Agregar host" (enroll-token, solo admin)
- PARCIAL Detalle de host: encabezado, tarjetas de ultimo valor, graficas CPU/Memoria/Red con rangos 1h..1y; NO muestra aun swap/load/bateria/discos/`info`/grupo/etiquetas de la API ampliada
- NO EMPEZADO Pantalla Resumen (`/`, contra `GET /summary`), tabla de Hosts con filtros/orden/paginacion (`Pagination`/`FilterChip` sin cablear), pantalla Grupos (`/grupos`) y etiquetas, detalle ampliado, items de nav Resumen/Grupos
- NO EMPEZADO Tests de frontend (sin runner), code-splitting
- Sin verificar: todo lo visual y de teclado (sin navegador), CSP real, rangos 30d/90d/1y y contrato nuevo contra la API real

## Infra
- PARCIAL `docker-compose.yml` solo con Postgres 18; scripts `scripts/check-{go,web}.sh`
- NO EMPEZADO CI (`-race`), Dockerfiles, empaquetado y firma, despliegue

## Bloqueos y decisiones abiertas
- Ver "Pendientes de decision del usuario" en `decisiones.md` y el resumen de `deuda-tecnica.md`.
- `CLAUDE.md` del repo aun describe terminal-dark; el estilo vigente es Slate.
