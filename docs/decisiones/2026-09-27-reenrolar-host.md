# B14 + B15: re-enrolar conservando historial / reactivar host y rotar token (2026-09-27)

## Problema
Antes, canjear un enroll token SIEMPRE creaba un host nuevo (`store.EnrollHost`): reinstalar el agente en el mismo equipo (`enroll --force`) dejaba el host anterior huérfano, perdiendo su historial de métricas/alertas. Tampoco había forma de reactivar un host `disabled` (su token quedó revocado al deshabilitarlo) sin recrearlo.

## Diseño: un solo mecanismo para los dos casos
`enroll_tokens` gana `target_host_id` (migración 00037, nullable). Canjear un token con `target_host_id` (`EnrollHost`) hace `UPDATE` sobre ese host en vez de `INSERT`: conserva `id` (y con él todo lo ligado — métricas, alertas, eventos, retención propia) y NO cambia su `group_id`. Sirve igual para reactivar un host `disabled` (B15) que para re-enrolar uno activo al que se le perdieron las credenciales o se reinstala en el mismo hostname (B14).

- **`POST /api/v1/hosts/{id}/re-enroll`** (admin): crea un enroll token targeted a ese host y responde igual que el alta normal (`enroll_token`, `expires_at`, `commands`). 404 si el host no existe, es de otro grupo, o es un dispositivo. Funciona con el host en cualquier estado.
- **Aviso de hostname duplicado** (parte de B14): `POST /hosts/enroll-token` (alta normal) agrega `hostname_conflict: {host_id, status}` a la respuesta 201 si ya existe un host `kind='agent'` con ese hostname (case-insensitive) — no bloquea, solo avisa. El campo se **omite** (no `null`) cuando no hay conflicto.
- Web: "Deshabilitar" (nuevo — antes no existía forma de deshabilitar un host agente desde el dashboard, solo dispositivos) y "Reactivar"/"Re-enrolar" (según estado) en el detalle del host, reusando el diálogo de token de "Agregar host" (extraído a `EnrollTokenStep`, compartido).

## Deuda / decisiones menores
- `host_events.source` no distingue re-enroll de alta nueva (reusa `'enroll'`; se diferencia solo por si `from_status` es NULL). Ampliar el CHECK en una migración futura si hace falta distinguirlo en el historial.
- El texto de ayuda de `argos-agent enroll --force` ("el host anterior queda huérfano") sigue diciendo eso — ya no es siempre cierto con tokens targeted, pero no se tocó (fuera de alcance de este lote, es solo un string de ayuda).
- Corregido de paso: `TestRuleScopeHostAndDeviceContract` era intermitente (nombre de regla por `time.Now().UnixNano()`, colisionaba con llamadas rápidas seguidas); ahora usa un contador además del nanosegundo.
