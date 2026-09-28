# B7: planes y cuotas por cliente (2026-09-27)

## Decisión de diseño (con el usuario): cliente = grupo, 1 a 1
Sin entidad nueva. `host_groups` gana 6 cuotas opcionales (migración `00039_group_quotas.sql`): `max_hosts`, `max_devices`, `max_checks`, `max_users`, `max_rules`, `max_channels` — todas `INT NULL` (sin límite propio; los topes globales de la plataforma que ya existían — 200 grupos, 200 reglas, 50 canales, etc. — siguen rigiendo igual, esto es un tope ADICIONAL por cliente). Sin "planes" con nombre por ahora: cada grupo fija sus propios números directamente (más simple; si más adelante se quieren plantillas reusables de plan, es una capa aparte sobre esto, no hace falta rehacer nada).

## Refuerzo (dónde se aplica la cuota)
- **Hosts** (`EnrollHost`): al crear un host nuevo con ese grupo (alta normal, alta masiva B16). Un re-enroll (B14/B15) no cambia de grupo, así que nunca suma.
- **Dispositivos** (`CreateDevice`/`UpdateDevice`): al crear con grupo, y al mover a un grupo DISTINTO (no al re-fijar el mismo ni al quitarlo).
- **Usuarios** (`CreateUser`/`UpdateUser`): por cada grupo NUEVO que se le agrega a un viewer (no al quitarle uno ni al mantener los que ya tenía).
- **Chequeos** (`CreateServiceCheck`) y **reglas** (`CreateAlertRule`): solo cuando el alcance es `group` — alcance `host`/`device`/`all` no cuenta contra la cuota del grupo en esta versión (simplificación deliberada).
- **Canales** (`CreateChannel`): solo si el canal tiene `group_id` (uno global nunca cuenta).

Todos los chequeos corren dentro de la misma transacción del alta, y el error (`ErrGroupQuota`/`GroupQuotaError`) se mapea a 409 `group_quota_exceeded` con `{"resource","current","limit"}`.

## Consumo visible
`GET /api/v1/groups/{id}/usage` (admin) da el consumo actual de los 6 recursos frente a su cuota. El listado de grupos (`GET /groups`) ya trae los 6 límites configurados en `quotas`. Web: sección "Cuotas del cliente" en el formulario de editar grupo (6 campos, vacío = sin límite), con el uso actual como pista junto a cada campo.

## Bug real encontrado y corregido (no aplazado: afectaba el propio contrato del endpoint)
El DTO de `GET /groups/{id}/usage` no tenía `json` tags: serializaba con mayúsculas (`Hosts`, `Devices`...) en vez de minúsculas. Corregido con test de contrato (`TestGroupUsageShape`) que fija las claves esperadas.

## Deuda
- ~~Mover un HOST entre grupos no aplicaba `max_hosts`~~ — **corregido 2026-09-27**: `checkGroupMove` (compartida por `SetHostGroup` y `BulkHosts set_group`) ahora cuenta cuántos de los hosts movidos de verdad CAMBIAN de grupo (agentes contra `max_hosts`, dispositivos contra `max_devices`; re-fijar el mismo grupo o quitarlo nunca suma), con test (`TestGroupQuotaOnMove`).
- Sin tests end-to-end de bloqueo real por cuota en ninguno de los 6 recursos: **cerrado 2026-09-27** (`quota_test.go`, ver `tests-pendientes.md`).
- La cuota de chequeos/reglas con alcance `host`/`device` no cuenta contra el grupo del host — simplificación deliberada, podría revisarse si se vuelve un problema real.
- El error de cuota al editar un usuario/dispositivo/etc. no tiene un mensaje especial en cada diálogo del web (solo el `GroupFormDialog` lo tiene, y ahí en realidad nunca puede ocurrir); en el resto se muestra el mensaje genérico del servidor, que ya es descriptivo.
