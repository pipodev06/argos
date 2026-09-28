# B19-B30, bloque "medio": `GET /metrics`, flapping, constancia de baja

Segunda tanda de funcionalidad secundaria (2026-09-27). Sigue pendiente, por decisión del usuario, todo el
bloque "grande" (SMTP, MFA/sesiones/recuperación, informes PDF/programados, i18n, accesibilidad/móvil,
DR/runbook, invitaciones+rol delegado). **Escalamiento de alertas: sin hacer, pendiente de decisión** (¿avisar
a canales distintos tras N tiempo sin reconocer — implica evento `escalation` nuevo, tabla de canales de
escalamiento y columna en la regla — o solo subir la severidad, que entra en el ciclo de recordatorios
existente?).

## A. `GET /metrics` (texto Prometheus)
Deshabilitado por defecto: sin `ARGOS_METRICS_TOKEN` responde 404. Con token (≥ 32 caracteres, se valida al
arrancar) exige `Authorization: Bearer`, comparación en tiempo constante; ausente/incorrecto → 401 sin
cuerpo. Ruta pública (fuera de sesión/CSRF), clasificada explícitamente en `permissions_enum_test.go` y
`security_test.go`. Solo agregados globales, sin hostnames ni nombres de grupo/canal en labels (probado con
datos sembrados). Métricas: `argos_build_info`, `argos_uptime_seconds`, `argos_task_last_ok_age_seconds`,
`argos_notification_queue_*`, `argos_notification_dead_24h`, `argos_db_size_bytes`, `argos_hosts{status}`,
`argos_alerts_open{severity}`. Formato escrito a mano (sin `prometheus/client_golang`). Sin rate-limit ni
auditoría (decisión del agente; revisar si se expone a internet sin túnel). `argos_hosts` cuenta agentes y
dispositivos (el resumen del dashboard solo cuenta agentes).

## B. Flapping
3.ª apertura de la misma regla y host dentro de 30 min → `alerts.flapping = true` y su `opened` queda
`suppressed` (constantes `FlapWindow`/`FlapThreshold` en `alert_eval.go`, no configurables). Columna aparte de
`alerts.suppressed` (esa es de mantenimiento y `releaseSuppressedSQL` la liberaría mal). Como el `opened`
queda suppressed, tampoco se envía el `resolved` ni hay recordatorios. Migración `00041`. UI: badge
"Intermitente" (icono + texto, tinte muted) en la tabla y el detalle de alertas, con la línea "Se abrió 3 o
más veces en 30 minutos; no se envió aviso." Tests: `alert_flapping_test.go` (3.ª abre flapping; 1 previa o
previas fuera de ventana, normal; otro host no cuenta).

## C. Constancia de baja de cliente
El purge de grupo devuelve además `group_name`, `purged_at`, `performed_by`. La UI ofrece "Descargar
constancia" (txt generado en el navegador, `buildConstancia`/`constanciaFilename` en `purge.ts`, helper
`web/src/lib/download.ts`); lleva cliente, fecha, ejecutor y conteos, sin hostnames ni ids (hosts fallidos
solo como cantidad). No es un documento firmado: es texto generado por el cliente a partir de la respuesta
del server (la evidencia autoritativa sigue siendo el registro de auditoría `group_purged`).

## Deuda
- Umbrales de flapping fijos. Escalamiento sin hacer. Visual y descarga real no probados en navegador.
