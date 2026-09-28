# B19-B30, bloque "bajo esfuerzo": plantillas de reglas, export CSV de inventario/parches, silenciar regla

Primer bloque de la funcionalidad secundaria (post-core), triada en bajo/medio/grande (2026-09-27). Grande
(MFA, SMTP, recuperación de contraseña, informes PDF, i18n, accesibilidad/móvil, DR/runbook,
invitaciones+rol delegado) queda pendiente sin tocar por decisión del usuario. Medio (flapping/
escalamiento, `GET /metrics`, constancia de baja) es la siguiente tanda.

## 1. Plantillas de reglas de alerta
Solo frontend. `RULE_TEMPLATES` en `ruleForm.ts` (6 presets: CPU alto, memoria alta, disco casi lleno,
host caído, sin reportar, actualizaciones de seguridad pendientes). Selector "Plantilla" en
`RuleFormDialog` visible solo al crear (no al editar/clonar); elegir una reemplaza el borrador completo,
el nombre queda vacío para que el usuario lo ponga. Sin cambios de API.

## 2. Exportar inventario y parches como CSV
Nuevas rutas `GET /hosts.csv`, `/devices.csv`, `/updates.csv` (mismo filtro que su lista, sin paginación/
orden), reusando `csvExport`/`csvSafe` de `reports_csv.go`. Tope `maxInventoryExportRows=5000` (corte
silencioso, mismo patrón que `maxGroupExportHosts`). Store en `inventory_export.go`
(`ExportHosts`/`ExportDevices`/`ExportUpdatesFleet`); se refactorizaron los filtros de `ListHosts`/
`ListDevices`/`ListUpdatesFleet` a funciones `*FilterWhere` compartidas para no duplicar la traducción a
SQL. Auditado con el evento existente `report_exported` (no se creó uno nuevo). `devices.csv` no lleva
columna `os` (el dispositivo no tiene ese campo): nombre, dirección, colector, perfil SNMP, grupo, estado,
visto. Botón "Exportar CSV" en Hosts/Dispositivos/Parches (siempre habilitado, sin gate de "sin datos"
como Informes). **Sin test propio contra Postgres real** — aplazado igual que B17 en su momento, anotado
en `docs/tests-pendientes.md`.

## 3. Silenciar una regla de alerta puntual
Distinto de "mantenimiento" (por host/grupo, ya existente): esto silencia solo las notificaciones NUEVAS
de una regla puntual por 15 min/1 h/4 h/24 h, sin desactivar su evaluación — la alerta se sigue abriendo,
trackeando y resolviendo normal, solo no se manda a los canales mientras `muted_until` está en el futuro.
Las notificaciones `resolved` nunca se silencian. Migración `00040` (`alert_rules.muted_until`). Enganche
en `alert_eval.go`: `openAlertsSQL` (CTE `enq`) y `remindersSQL` unen `alert_rules` y agregan la condición
de mute a su CASE; `enqueueResolvedCTE` no se tocó (a propósito). Rutas `POST /alert-rules/{id}/mute`
`{minutes: 15|60|240|1440}` y `/unmute`, `accessAdmin`, auditadas con `evAlertRuleUpdated` +
`detail.action`. UI en `RulesTable`: botón "Silenciar"/"Quitar silencio" + badge "Silenciada hasta HH:mm".
**Sí lleva tests** (motor de alertas = core): `alert_rule_mute_test.go` confirma que con mute activo la
alerta abre igual pero la entrega `opened` queda `suppressed`, que `resolved` no se silencia, y el caso
`ErrNotFound`.

## Verificación
`check-go.sh server` y `check-web.sh` en verde tras integrar ambas partes (contrato de rutas/campos
`muted_until` y de los 3 endpoints `.csv` congelado antes de lanzar los 2 agentes en paralelo).
