# B16 + B17: alta masiva de hosts, exportar cliente y limpiar canales/perfiles al purgar (2026-09-27)

## B16: token de grupo para alta masiva (migración 00038)
`enroll_tokens` gana `max_uses`/`uses_count` y `hostname` pasa a ser nullable (con `hostname IS NOT NULL OR group_id IS NOT NULL`). Un token con `max_uses > 1` no tiene hostname fijo: cada agente que lo canjea manda el suyo (`agent/internal/enroll` ahora envía `os.Hostname()`), y el host resultante nace en el grupo del token. El token sigue sirviendo hasta agotar `max_uses` (bloqueo de fila para que la concurrencia no se pase del tope); `used_at` solo se fija al agotarse del todo, así que un token de grupo a medio usar sigue listado como "pendiente" y es revocable.

- `POST /hosts/enroll-token` body: `{hostname, group_id?, max_uses?}` — `max_uses` ausente o 1 = alta normal (hostname obligatorio); `max_uses` 2..500 = alta masiva (`group_id` obligatorio, `hostname` se ignora).
- Validación de rango/obligatoriedad vive en el handler HTTP (el CHECK de la BD es solo el respaldo).
- No se puede combinar con `target_host_id` (re-enroll, B14/B15): 400 `invalid_body`.
- Web: casilla "Alta masiva" en "Agregar host" (grupo obligatorio, campo "cuántos equipos"); el token resultante sirve el mismo comando para varios equipos. `PendingTokensDialog` muestra "Alta masiva" en vez de un hostname y una columna "Usos" (`N / max`).

## B17: exportar el cliente antes de purgar + limpieza de canales/perfiles SNMP
- `GET /api/v1/groups/{id}/export.zip` (admin): zip con `hosts.csv` (hosts y dispositivos del grupo, sin límite de página — tope interno `maxGroupExportHosts=5000`, sin 422 si se excede, deuda anotada) y `disponibilidad.csv` (reusa la misma lógica que `availability.csv`, rango 30 días). Mismo cupo `reportSem` que el resto de los informes. Auditado (`evGroupExported`, solo conteos).
- Al purgar un grupo (`purgeGroupConfig`), además de lo que ya se borraba (reglas, ventanas, chequeos, tokens), ahora también: `notification_channels` con `group_id` del grupo, y los perfiles SNMP que quedaron sin ningún host que los use tras purgar los dispositivos del grupo (un perfil compartido con otro grupo sobrevive). `PurgeGroupResult`/el JSON de la API ganan `channels` y `snmp_profiles`.
- "Purga verificable" (parte del ticket B17) ya estaba resuelta desde antes: la API y la web ya muestran conteos completos y quedan en `audit_log`; no hizo falta tocar nada ahí.
- Web: botón "Exportar antes de purgar" en el diálogo de baja de cliente, antes de escribir el nombre de confirmación.

## Deuda
- El tope de 5000 hosts en el export no tiene test que lo fuerce ni da un error explícito si se supera (solo corta en silencio).
- El texto de ayuda de `argos-agent enroll --force` sigue sin actualizarse (deuda ya anotada en el lote de B14/B15).
