# Escalamiento de alertas y borrado de hosts/dispositivos por tandas

## Escalamiento (migración 00042)
Decisión del usuario (2026-09-27): hacer las dos variantes a la vez. Por regla, `escalate_after_seconds`
(60..604800, NULL = sin escalamiento). Cuando una alerta `open` (no reconocida) supera ese plazo, una sola vez
(`alerts.escalated_at`): (b) su severidad sube un nivel (info→warn, warn→critical; critical se queda), lo que la
mete en el ciclo de recordatorios existente; (a) se encola un aviso `escalation` (verbo ESCALADA) a los
**canales de escalamiento** de la regla (`rule_escalation_channels`, `PUT /alert-rules/{id}/escalation-channels`,
mismas validaciones de dueño/grupo que los canales normales). Cada parte es opcional. No escalan alertas
reconocidas, en mantenimiento (`suppressed`) ni intermitentes (`flapping`). El `resolved` también llega a los
canales de escalamiento. Un solo nivel, sin cadenas ni horarios. UI: campo "Escalar si nadie reconoce en
(minutos)" + segundo selector de canales en el formulario de regla; badges "Escalada" y "Escala a los N min".
Auditoría: `alert_rule_channels_set` con `detail.list` = `channels` | `escalation_channels`. Ambos PUT devuelven
`channel_ids` deduplicados tal como llegaron.

## Borrar un dispositivo fallaba con 500 (bug)
`DeleteDevice` hacía un `DELETE FROM hosts` directo: `service_checks` (y `metrics`, `disk_samples`, …) tienen FK
sin CASCADE, así que cualquier dispositivo con historial de chequeos daba `internal server error`. Como el
agente que hace de colector no se puede purgar mientras tenga dispositivos, eso dejaba bloqueada también la
purga del agente (409 `collector_has_devices`, por diseño). Arreglo: `DeleteDevice` usa la misma limpieza que la
purga de host. Tests: `TestDeleteDeviceWithHistory`.

## Borrados grandes por tandas
Medición (3,6 M filas de un host, transacción única): 5,4 s y 407 MB de WAL; escala lineal, con ~30 M filas
serían ~4 GB de WAL en una transacción (con `max_wal_size` de 1 GB) y más de un minuto con el host bloqueado. La
retención actual (detalle crudo 8 d, chequeos 14 d) hace poco probable ese volumen por host, pero no imposible
(chequeos frecuentes). Ahora `purgeHostBatched` (usado por `PurgeHost`, `DeleteDevice` y `PurgeGroup`) borra
`metrics`, `service_checks` y `disk_samples` por ventanas de un día (una transacción por ventana) y luego, en una
transacción corta, lo restante y el host. Si se interrumpe queda un host con menos historial y repetir continúa.
Lo que puede rechazarse (colector con dispositivos) se comprueba antes de borrar nada. El WAL total no baja, pero
ya no hay una transacción gigante. Los handlers de baja amplían el plazo de escritura (`extendPurgeDeadline`).
Tests: `TestPurgeHostBatchedByDay`.

## Deuda
- Sin prueba con decenas de millones de filas reales; la purga sigue siendo síncrona en la petición HTTP (plazo
  30 min). Si un día hace falta, pasarla a tarea en segundo plano con progreso.
- Tras un borrado grande quedan tuplas muertas hasta que autovacuum pase (no se fuerza VACUUM).
