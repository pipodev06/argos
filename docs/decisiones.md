# Decisiones (INDICE: leer primero; abrir solo el tema de tu tarea)

## Como se usa este directorio
- Este archivo es solo indice (<=100 lineas). El detalle vive en `docs/decisiones/<tema>.md`; abre unicamente los temas de tu tarea.
- Decision nueva = archivo propio `docs/decisiones/AAAA-MM-DD-tema.md` (o ampliar el tema existente si es del mismo asunto).
- Los agentes NO escriben en `docs/`: devuelven "notas para docs" y el orquestador las vuelca y anade la linea al indice.
- Cada linea: `[estado: vigente | pendiente-usuario | reemplazada]` + tema + enlace. Reemplazada -> mover el detalle a `archivo.md` con una linea de por que.
- No duplicar datos entre archivos: enlazar. No agregar logs de tests/e2e (eso es git).
- Lee tambien `estado-del-proyecto.md` (<=60 lineas) y el resumen de `deuda-tecnica.md`.

## Pendientes de decision del usuario
- Separar rol de BD migrador/runtime (hoy el trigger append-only de `audit_log` se puede saltar). Recomendado, no decidido -> [auth](decisiones/auth-y-sesiones.md)
- MFA (TOTP/WebAuthn), rotacion automatica del token de agente y auditoria transaccional: no decididas ni implementadas -> [auth](decisiones/auth-y-sesiones.md)
- NFKC en contrasenas: requiere `golang.org/x/text` (dependencia no aprobada) -> [auth](decisiones/auth-y-sesiones.md)
- Lista de contrasenas comprometidas embebida (SecLists 10k, MIT): confirmar; aporta casi nada con minimo 15 -> [auth](decisiones/auth-y-sesiones.md)
- `x/crypto` con GO-2026-5932 (govulncheck, sin version corregida, codigo no la llama): aceptar o cambiar de libreria -> [deuda](deuda-tecnica.md)
- Persistir buffer offline del agente en disco: elegir libreria (SQLite u otra) -> [agente](decisiones/agente-transporte-y-estado.md)
- Firma de binarios (SmartScreen/Gatekeeper) y auto-actualizacion (solo con firmas): no antes -> [servicio](decisiones/agente-colectores-y-servicio.md)
- Etiqueta launchd `com.argos.agent` y rutas de instalacion (`/opt/argos-agent`, `/usr/local/libexec/argos-agent`, `%ProgramFiles%\argos-agent`) -> [servicio](decisiones/agente-colectores-y-servicio.md)
- `charging` = conectado a corriente: confirmar con `web/` y motor de alertas -> [servicio](decisiones/agente-colectores-y-servicio.md)
- Gracia de arranque del detector (no marca nada los primeros `DOWN_AFTER`): confirmar o quitar -> [estados](decisiones/estados-y-deteccion.md)
- Reclasificacion down<->offline al cambiar de grupo/`always_on` sin mover `status_changed_at`: confirmar -> [estados](decisiones/estados-y-deteccion.md)
- Migraciones 00012/00013 (propias del backend) y vocabulario CHECK de status: pendientes de revision por `data-infra-engineer` -> [grupos](decisiones/grupos-etiquetas-metricas.md)
- Respaldar `argos` antes de reiniciar el server (00006 toma `ACCESS EXCLUSIVE`; migrar 00006-00013 a la BD real) -> [datos](decisiones/datos-retencion-particionado.md)
- CSP y `<style>` inyectados por Radix (scroll de fondo con modal): aceptar degradacion o nonce por request -> [csp](decisiones/csp-y-cabeceras.md)
- Tests de frontend: adoptar Vitest (no hay runner) -> [datos front](decisiones/frontend-datos-y-navegacion.md)
- Estilo a confirmar: radio circular de radio buttons (excepcion al 6px), checkbox neutro, tooltip, titulos `text-sm`, marcas UsageBar; `pending`=`info`; badge sin pastilla -> [estilo](decisiones/frontend-estilo-y-componentes.md)
- CLAUDE.md del repo aun describe `terminal-dark` (y no menciona Slate); decisiones citan reglas de Slate en CLAUDE.md que no estan alli. Actualizar CLAUDE.md (solo el usuario/orquestador) -> [archivo](decisiones/archivo.md)
- Bundle ~660 kB (Recharts): code-splitting del detalle o uPlot -> [datos front](decisiones/frontend-datos-y-navegacion.md)
- Servir SPA desde el binario Go (fallback `index.html`, cache, CSP) y servir instaladores: sin disenar/implementar -> [csp](decisiones/csp-y-cabeceras.md)
- CI Linux con `go test -race` (sin gcc en Windows local) y empaquetado: `devops-engineer` -> [operacion](decisiones/operacion-y-tooling.md)
- Historial de transiciones `host_events` (base de alertas/uptime) y motor de alertas: sin disenar -> [estados](decisiones/estados-y-deteccion.md)
- `ARGOS_CURSOR_KEY` en config si se escala a varias replicas (hoy clave por proceso) -> [api](decisiones/api-servidor.md)

## Indice de temas vigentes
- [estado: vigente] Auth de dashboard: roles, sesion `__Host-argos_sid`, Argon2id, bloqueo/rate limit, CSRF, usuarios, auditoria, TLS, cliente web de login -> [auth-y-sesiones](decisiones/auth-y-sesiones.md)
- [estado: vigente] Cabeceras de seguridad, CSP, Host allowlist, `<style>` de Radix/Recharts, fallback SPA -> [csp-y-cabeceras](decisiones/csp-y-cabeceras.md)
- [estado: vigente] Base servidor: goose, vocabulario CHECK, errores, limites, series de detalle, rangos 1h-1y, lista de hosts keyset, ingest -> [api-servidor](decisiones/api-servidor.md)
- [estado: vigente] Estados up/down/offline/disabled, detector, umbral por grupo, reclasificacion, `status_changed_at` -> [estados-y-deteccion](decisiones/estados-y-deteccion.md)
- [estado: vigente] Grupos, etiquetas, resumen, migraciones 00007-00013, metricas nuevas (swap/load/bateria/discos), contratos y errores -> [grupos-etiquetas-metricas](decisiones/grupos-etiquetas-metricas.md)
- [estado: vigente] Particionado diario UTC, `metrics_hourly`, mantenimiento, guardia de borrado, retenciones -> [datos-retencion-particionado](decisiones/datos-retencion-particionado.md)
- [estado: vigente] Agente: metricas base, enroll, estado, transporte seguro, resiliencia, exit codes -> [agente-transporte-y-estado](decisiones/agente-transporte-y-estado.md)
- [estado: vigente] Agente: colectores (swap/load/bateria/discos/host_info) y servicio nativo Windows/Linux/macOS -> [agente-colectores-y-servicio](decisiones/agente-colectores-y-servicio.md)
- [estado: vigente] Frontend: estilo Slate, tema por boton, shell/sidebar, componentes compartidos, estados de host -> [frontend-estilo-y-componentes](decisiones/frontend-estilo-y-componentes.md)
- [estado: vigente] Frontend: `lib/api.ts`, polling, react-router 8.4.0, detalle con graficas y rangos -> [frontend-datos-y-navegacion](decisiones/frontend-datos-y-navegacion.md)
- [estado: vigente] Variables `ARGOS_*` (tabla verificada contra el codigo), tests con BD real, compose, e2e, `-race` -> [operacion-y-tooling](decisiones/operacion-y-tooling.md)
- [estado: reemplazada] terminal-dark, navegacion por hash, tema por `prefers-color-scheme`, icacls, tokens en claro, cursor, etc. -> [archivo](decisiones/archivo.md) (archivo, no leer salvo que investigues historia)

- [vigente] Fases 2 y 3: pantallas con datos reales e instaladores servidos -> [decisiones/2026-09-24-fases-2-y-3.md](decisiones/2026-09-24-fases-2-y-3.md)

- [vigente] Alertas y notificaciones (diseno aprobado, en revision de seguridad) -> [decisiones/2026-09-24-alertas.md](decisiones/2026-09-24-alertas.md)

- [vigente] Chequeos de servicios: diseno aprobado y tanda 1 (servidor+agente) implementada -> [decisiones/2026-09-24-chequeos-servicios.md](decisiones/2026-09-24-chequeos-servicios.md)

- [vigente] Etiquetas de host ELIMINADAS (migracion 00018): reemplaza las decisiones de etiquetas; los grupos se conservan -> [decisiones/2026-09-24-fases-2-y-3.md](decisiones/2026-09-24-fases-2-y-3.md)
- [2026-09-25 Permisos por grupo](decisiones/2026-09-25-permisos-por-grupo.md) — ámbito por usuario/grupo, tanda 1 hecha (hosts/resumen/grupos/usuarios), tandas 2 y 3 pendientes
- [2026-09-25 SLA y CSV](decisiones/2026-09-25-sla-csv.md) — diseño aprobado del punto 7 (host_events, disponibilidad, informes CSV); implementación en 3 tandas
- [2026-09-25 Ping y SNMP](decisiones/2026-09-25-ping-snmp.md) — diseño aprobado del punto 8 (dispositivos sin agente, agente colector, SNMP v3, gosnmp); se implementa después del 7
- [2026-09-25 Inventario del host](decisiones/2026-09-25-inventario.md) — puertos/servicios/procesos por agente, permiso can_inventory, 4 tandas (agente, ingest, lectura, UI)
- [2026-09-25 Parches y vulnerabilidades](decisiones/2026-09-25-parches.md) — diseño propuesto (fase 1: actualizaciones pendientes/seguridad/reinicio/EOL; CVE aplazado), 4 tandas, pendiente de aprobación
