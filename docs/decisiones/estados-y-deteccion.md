# Estados de host y deteccion de caidos (2026-09-23/24)
Contexto: `hosts.status` in `pending|up|down|offline|disabled`. `disabled` = revocado (ver `auth-y-sesiones.md`).

## Detector (`cmd/server/hostdown.go`)
- Goroutine con el mismo dueno y ciclo de vida que la purga (`maintCtx` + `WaitGroup` en `serve`; termina al cancelar el contexto, tambien con una consulta en vuelo). Cada `ARGOS_HOST_CHECK_INTERVAL` (15 s) ejecuta `Store.MarkStaleHosts` (antes `MarkStaleHostsDown`; devuelve el estado nuevo): un solo `UPDATE ... RETURNING` atomico e idempotente; con varias instancias la segunda re-evalua el WHERE y no ve el host como `up` (test con 4 llamadas concurrentes). Nunca toca `disabled`, `pending`, hosts ya caidos ni `up` sin `last_seen_at`.
- Estados: `up` que deja de reportar pasa a `down` (grupo `always_on`) u `offline` (resto y sin grupo); umbral = `down_after_seconds` del grupo o el global. El ingest devuelve a `up` desde down/offline (nunca `disabled`).
- Reloj: el umbral usa `now()` de Postgres (el que escribe `last_seen_at`), no el del proceso (evita falsos down por desfase). Los tests siembran `last_seen_at` relativo a `now()`.
- Config: `ARGOS_HOST_DOWN_AFTER` (default 2m, minimo 30 s) y `ARGOS_HOST_CHECK_INTERVAL` (default 15s, minimo 1 s; no puede superar `DOWN_AFTER`). Formato de duracion de Go.
- **Gracia de arranque (PENDIENTE DE CONFIRMAR)**: durante los primeros `DOWN_AFTER` desde que arranca el server el detector no marca nada (evita marcar todos los hosts al reiniciar server/BD y notificaciones falsas del futuro motor de alertas). Costo: una caida real se detecta hasta `DOWN_AFTER` mas tarde tras un reinicio. Usa el umbral GLOBAL. Quitarla = borrar `graceUntil` en `runHostDownDetector`.
- Recuperacion: el UPDATE del ingest fija `status_changed_at` solo si el status cambia (`CASE WHEN status <> 'up'`) y devuelve hostname + status previo (subselect `FOR UPDATE`, previo real aunque corra el detector). El handler loguea `host recuperado` si el previo era `down`; desde `offline` es DEBUG (laptops que despiertan).
- Logs (slog, sin secretos): `host caido` WARN (`host_id`, `hostname`, `down_after`) solo para down; offline es INFO. Sin tabla de eventos ni notificaciones (motor de alertas).
- Migracion 00005: `hosts.status_changed_at TIMESTAMPTZ NOT NULL DEFAULT now()`, backfill `COALESCE(last_seen_at, created_at)`; expuesto en `GET /hosts` y `GET /hosts/{id}`. Enroll inserta `up` (el DEFAULT es su instante).
- Sin indice en `last_seen_at`: 200 hosts seq scan 0.03 ms; 50 000 hosts 4.3 ms cada 15 s. Indexarla impide HOT updates (se escribe en cada ingest) y agrega bloat al camino caliente. Revisar con >100 000 hosts.
- Un `up` con `last_seen_at` NULL no se juzga (solo por edicion manual de BD).
- Futuro: tabla `host_events` (host_id, from_status, to_status, at), alimentada por detector e ingest en la misma transaccion; base del motor de alertas y de uptime. `status_changed_at` es solo el ultimo cambio.

## Reclasificacion al cambiar de grupo o `always_on` (PENDIENTE DE CONFIRMAR por el usuario)
- En la misma transaccion, los hosts `down`/`offline` afectados se re-etiquetan (down<->offline) segun el `always_on` del grupo destino (sin grupo = offline). `status_changed_at` NO se mueve (no es transicion de disponibilidad). `up`, `pending` y `disabled` no se tocan; los `up` se reevaluan en el siguiente ciclo (15 s) con el umbral nuevo.
