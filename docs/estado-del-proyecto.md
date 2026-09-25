# Estado del proyecto (actualizado 2026-09-25) — LEER ESTO AL INICIAR UNA SESIÓN NUEVA
Detalle por tema en `decisiones/` (índice: `decisiones.md`; abre solo el tema de tu tarea). Deuda: `deuda-tecnica.md`. Historia: git. Reglas del orquestador y lecciones de agentes: memoria del proyecto y `~/.claude/agents/*.md`.

## Qué es
Monitoreo tipo Zabbix/PRTG: agente Go multiplataforma (`agent/`) + servidor Go/Postgres 18 (`server/`, goose 00001–00021) + dashboard React (`web/`, Vite, Tailwind v4, shadcn, estilo Slate congelado en `CLAUDE.md`). Repo privado `pipodev06/argos` (rama `main`; CI de GitHub Actions verde: web, go server, go agent con `-race` en Linux).

## Hecho (implementado y probado con tests; lo visual NO está probado en navegador)
- Server: auth (admin/viewer, cookies, CSRF, auditoría), enroll de agentes, ingest, detector de caídos, grupos, resumen, retención y particionado; motor de alertas (reglas metric/host_status/no_data/service_check, mantenimiento, canales webhook con `/slack` para Discord); chequeos de servicios (tcp/http/tls/servicio, ejecutados por el agente); permisos por grupo (`store.Scope` obligatorio; viewers restringidos; canales/auditoría solo admin); `host_events` (00014 + 00020, retención 400 d); informes de disponibilidad (`/reports/*`, CSV); inventario del host (ingest 00021 + lectura con `can_inventory`); frontend servido por el server con `ARGOS_WEB_DIR`; etiquetas eliminadas.
- Agente: métricas, servicio nativo, chequeos con política local `ARGOS_CHECK_ALLOW`, inventario (`ARGOS_INVENTORY_MODULES`, apagado por defecto), reintento del reemplazo atómico de `agent.json`.
- Web: hosts (filtros, detalle, inventario), grupos, chequeos, alertas/reglas/mantenimiento/canales, usuarios con permisos, auditoría, Informes (`/informes`); Vitest (72+ tests).

## En curso / siguiente (orden acordado)
1. Botones "Exportar CSV" y filtro por grupo en Informes (`web/`; contrato en `decisiones/2026-09-25-sla-csv.md`).
2. Punto 2: parches/vulnerabilidades del SO (falta diseño).
3. Punto 8: ping/SNMP para equipos sin agente (diseño aprobado: `decisiones/2026-09-25-ping-snmp.md`, `gosnmp` aprobado, 3 tandas).
4. Servicios Linux del inventario (v1.1, `systemctl`), alerta "puerto nuevo expuesto" (diseñada).
Aplazado por el usuario: canal SMTP, despliegue/dominio, separar roles de BD, firma de binarios. Sin probar: instalación real del agente (Windows/Linux/macOS), systemd sin privilegios, revisión visual de pantallas nuevas (la hace el usuario).

## Cómo trabajar y verificar
- Verificar: `bash scripts/check-go.sh <server|agent> [--fast] [--pkg ./ruta]` y `bash scripts/check-web.sh`. Los tests de BD se SALTAN sin `ARGOS_TEST_DATABASE_URL=postgres://argos:argos@localhost:5432/argos_test?sslmode=disable` (BD `argos_test` ya existe en el Postgres local): definirla SIEMPRE.
- Orquestación: medir antes de delegar; spec larga en archivo del scratchpad y prompt corto; agentes no escriben `docs/` (devuelven "Notas para docs"); ≤2 agentes en paralelo con contrato fijo; revisar lo entregado con los scripts y greps de estilo.
- Levantar en local (Windows, Git Bash): BD `argos` (usuario y clave `argos`, localhost:5432). Clave de cifrado estable en `C:\Users\piter\.argos-dev\secret-key.txt` (si se pierde hay que recrear los canales). Server: compilar `cd server && go build -o <ruta>/argos-server.exe ./cmd/server` y ejecutar con `ARGOS_WEB_DIR=D:/projects/argos/web/dist ARGOS_SECRET_KEY=$(cat ~/.argos-dev/secret-key.txt) ARGOS_AGENT_DIST_DIR=D:/projects/argos/dist/agent ARGOS_PUBLIC_URL=http://localhost:8080 DATABASE_URL=postgres://argos:argos@localhost:5432/argos?sslmode=disable`; agente: `agent` compilado con `ARGOS_INTERVAL=30s ... run`; frontend: `cd web && npm run build` (el server sirve `web/dist`). Claude Code mata procesos en segundo plano bajo presión de memoria: reiniciarlos solo cuando el usuario lo pida.
