# Archivo: decisiones reemplazadas o resueltas (NO leer salvo que investigues historia)
Formato: que era -> por que ya no vale -> donde vive lo vigente. Los logs cronologicos de `estado-del-proyecto.md` (detalle de tests, e2e, rutas) se descartaron en la reestructuracion de 2026-09-24: es historia de git.

## Frontend / estilo
- **Estilo `terminal-dark`** (dark-only, tokens `bg #0b0f0d`, `surface #121613`, `accent #3ed67a`, `danger #ff5c5c`, mono+sans, un solo mecanismo de elevacion = borde 1px, sin sombras) -> reemplazado el 2026-09-23 por Slate claro/oscuro (Inter, `brand`/`ok`/`warn`/`danger`/`info`, radio 6px) -> `frontend-estilo-y-componentes.md`. Plantilla original: `~/.claude/projects/shared-memory/reference_style_terminal_dark.md`. OJO: `CLAUDE.md` del repo aun lo describe (ver indice).
- Modal terminal-dark (overlay `bg-bg/80`, `border-accent`, sin ring/sombra/blur) y status `disabled` = tag `[!]` en `text-text-muted` -> sustituidos por Dialog Slate (`shadow-lg`, overlay `bg-black/50`) y `IconCircleOff`.
- Tema Slate por `@media (prefers-color-scheme: dark)` sin toggle -> reemplazado por tema solo por boton (`.dark`, `argos-theme` en localStorage).
- Dialog: "sombra suave (`shadow-lg`) unica excepcion" -> ampliado a dialog/alert-dialog/sheet/popover/dropdown (select y tooltip sin sombra).
- Paleta de graficas `--chart-*` "pendiente" -> cerrada con el detalle de host (`--chart-1..5`).
- "Sin React Router: `AuthGate` decide pantalla por estado" -> `AuthGate` sigue decidiendo login/cambio forzado/dashboard, pero las rutas del dashboard usan react-router.
- **Navegacion por hash sin libreria** (`hooks/useHashRoute.ts`, `#/`, `#/hosts/<id>`, `hostHref`/`HOSTS_HREF`) -> reemplazada por react-router 8.4.0 (modo declarativo); los enlaces `#/hosts/<id>` ya no funcionan (cae a `/`; sin uso externo) -> `frontend-datos-y-navegacion.md`.
- Rangos de detalle solo `1h|6h|24h|7d` -> ampliados a `30d|90d|1y` (leen `metrics_hourly`).
- `DashboardPage` con lista + boton "Agregar host" en el header -> lista en `pages/HostsPage.tsx`, boton en la fila del titulo.

## Backend / datos
- **Enroll tokens en claro** -> RESUELTO: `00004` guarda SHA-256 (`token_hash`).
- **Retencion/particionado de `metrics`/`service_checks`** pendiente -> RESUELTO: `00006` + `internal/retention`. **TimescaleDB en compose** (PG16, otra password) -> ya no hace falta; compose usa `postgres:18`.
- **Modelo de auth de dashboard** pendiente -> RESUELTO: usuarios + sesion opaca en BD.
- **Paginacion por cursor en `GET /hosts`** (hoy `LIMIT 1000`) -> RESUELTO: keyset con `limit` 1..100 y `total`.
- Estado del host "solo pasa a `up`, nada lo marca `down`" -> RESUELTO: detector (`estados-y-deteccion.md`). "Un solo umbral global; laptops aparecen como `down`" -> RESUELTO: `offline` + umbral por grupo (`00007`/`00012`). `Store.MarkStaleHostsDown` -> renombrado `MarkStaleHosts`.
- "Bind `127.0.0.1` mientras enroll-token y `GET /hosts` no tengan auth" -> ya tienen auth; el default se mantiene por seguridad.
- Vocabulario `hosts.status` sin `offline` -> `00012` lo agrega.
- Tests de la API de series corridos contra una Postgres 18 temporal ("repetir contra `argos_test`") -> obsoleto (los corridos posteriores usan `argos_test`).

## Agente
- **ACL de Windows con `icacls.exe`** (System32, sin dependencias, best-effort, SYSTEM/Administrators fuera de la ACL) -> reemplazada por `SetNamedSecurityInfo` con SDDL (una operacion, acepta SID sin resolver) al agregar el servicio -> `agente-colectores-y-servicio.md`.
- **401 => salir con codigo 2 + `RestartPreventExitStatus=2`** -> vale solo en primer plano; el servicio queda inactivo y retoma tras `enroll --force`.
- Disco y load average "no se recolectan / no estan en el esquema" -> RESUELTO (colectores + `00009`/`00010`). Servicio del SO "sin implementar" -> RESUELTO (`install|uninstall|start|stop|status`). "Agente debe respetar `Retry-After` al vaciar el buffer" -> RESUELTO (`DrainPace`). Persistencia del agent token en disco -> RESUELTO (`agent.json`). Stub `CollectMetrics` -> gopsutil.
- "Un servicio SYSTEM no podria leer un estado enrolado por otro usuario" -> RESUELTO (ACL del servicio con lectura para `NT SERVICE\argos-agent`).
