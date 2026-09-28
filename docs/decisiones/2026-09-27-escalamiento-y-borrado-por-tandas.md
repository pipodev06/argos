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

## Ronda de correcciones tras la revisión de riesgos (2026-09-28)
- **Purgar un host que sigue reportando:** `purgeHostBatched` lo deshabilita (tras comprobar que no es colector con
  dispositivos) antes de borrar: el agente recibe 401 y deja de escribir. Si un reporte ya autenticado llega cuando
  el host desapareció, `IngestMetrics` devuelve `ErrHostGone` (FK 23503 + host inexistente) y el handler responde 401
  `invalid_token` en vez de 500 (el agente lo trata como token rechazado). Tests: `TestIngestHostGoneAndPurgeDisables`.
- **Silencio de regla que nunca avisaba (migración 00043):** una alerta que abría durante el silencio quedaba con su
  `opened` suppressed para siempre (sin aviso, sin recordatorios ni `resolved`). Ahora `alerts.mute_held` marca las que
  abrieron retenidas SOLO por el silencio (no flapping ni mantenimiento) y `releaseMutedSQL` libera su `opened`
  (`pending`) al terminar el silencio si siguen `open`; las reconocidas solo limpian la marca; con mantenimiento vigente
  no se libera hasta que terminen ambos. `EvalResult.MuteReleased`. Tests: `alert_mute_release_test.go`.
- **`argos_hosts`** ahora lleva `kind` (`agent`/`device`) además de `status`.
- **Tests que faltaban:** export CSV (`inventory_csv_test.go`, `inventory_export_test.go`: ámbito, fórmulas, filtro, tope
  5000) y escalamiento de punta a punta por el worker real (`escalation_worker_test.go`).
- **Flake corregido:** el PUT de canales devolvía los ids en orden de llegada tras el cambio del escalamiento y
  `TestRuleChannelAssignmentContract` fallaba la mitad de las veces; ahora se devuelven ordenados.
- Verificado con `check-go.sh server --race` (gcc de WinLibs en el PATH del shell) en verde.

## Medición con 10 M de filas y bajas simultáneas (2026-09-28, `scripts/perf/purge-volume.sh`)
Generador por SQL en una BD desechable (`argos_volume_test`), sin tocar la real. Host con 10 M filas (70 % chequeos,
20 % métricas, 10 % discos):
- Una sola transacción: 28,5 s, 1.211 MB de WAL, transacción más larga 28,5 s.
- Por tandas (primera versión): 69,5 s, 2.264 MB, transacción más larga **27 s** (el "arreglo" no cumplía): la
  transacción final repetía el `DELETE` por host en `metrics`/`service_checks`/`disk_samples` y recorría 10 M de
  entradas de índice ya muertas. Ahora esa pasada se limita a los últimos dos días (`tails` en `purgeHostTx`):
  **36,4 s, 1.868 MB, transacción más larga 9,9 s** (acotada por el tamaño de un día, no del host).
- El pool tiene 10 conexiones: 10 bajas simultáneas sin tope lo agotaban (una consulta trivial esperó hasta 2,19 s;
  164 sondeos en la corrida) aunque el total fuese 14,2 s. Con `maxConcurrentPurges = 2` (semáforo en el `Store`,
  cubre host, dispositivo y grupo; las demás esperan turno) el total sube a 26,7 s pero la peor consulta trivial es de
  7 ms. Tests: `TestPurgeWaitsForASlotWithoutTouchingTheHost`.
- `GET /metrics` tiene límite por IP (30/min, antes de validar el token). Test: `TestMetricsRateLimitPerIP`.
- Volumen real de chequeos: el historial de `service_checks` se muestrea (una muestra por chequeo cada 15 min sin
  cambio de estado, más una por transición; 14 días): unas 13 mil filas por host con 10 chequeos, no millones.

## Deuda
- Sin prueba con decenas de millones de filas reales; la purga sigue siendo síncrona en la petición HTTP (plazo
  30 min). Si un día hace falta, pasarla a tarea en segundo plano con progreso.
- Tras un borrado grande quedan tuplas muertas hasta que autovacuum pase (no se fuerza VACUUM).
