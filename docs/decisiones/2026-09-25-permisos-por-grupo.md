# Permisos por grupo (punto 6, 2026-09-25)

Diseño: `security-engineer` (fuera del repo, resumido aquí). Decisiones del usuario: un viewer restringido NO ve hosts sin grupo; auditoría y canales solo admin para viewers restringidos; el permiso de inventario (procesos/servicios) es POR USUARIO y apagado por defecto; ack/resolve de alertas ya era solo admin.

## Tanda 1 (hecha, verificada con `argos_test`): hosts, resumen, grupos, usuarios
- Migración 00019: `users.all_groups` (default true), `users.can_inventory` (default false), `user_groups` (FK CASCADE), CHECK `users_scope_viewer_only` (un admin tiene `all_groups` y no `can_inventory`). Los viewers existentes conservan acceso total.
- `store.Scope` (valor cero = no ve nada) es parámetro OBLIGATORIO en `ListHosts`, `HostByID`, `Summary`, `ListGroups`: el compilador impide olvidarlo. Se une con AND a filtros, total, contadores, facets, rankings. El ámbito se carga en cada request (sin caché de sesión): un cambio aplica a la siguiente petición. Vive en `sessionUser.Scope`.
- Fuera de ámbito = 404 idéntico al de un id inexistente. La huella del cursor incluye `Scope.Key()`. `GET /hosts/{id}/metrics` también se restringe (solo con ámbito restringido). Restringido: `ungrouped.host_count` = 0.
- PATCH/GET `/users`: `all_groups`, `group_ids` (≤200, sin repetidos), `can_inventory`. Promover a admin reinicia el ámbito; pedir restringir a un admin, o `group_ids` con `all_groups=true`, da 400 `invalid_scope`. Auditoría `user_permissions_changed` (ids solo si ≤20; conteos siempre). Cuerpo del PATCH hasta 16 KiB (200 UUID no caben en 4 KiB: bug hallado por los tests).
- Pendientes: tanda 2 (alertas, eventos, chequeos, reglas, mantenimiento, canales, auditoría) y tanda 3 (UI Usuarios, guard `inventory`). Hasta la tanda 2, un viewer restringido AÚN ve alertas/eventos/chequeos de hosts ajenos: no restringir usuarios reales antes de terminarla.
- Tests de BD: `ARGOS_TEST_DATABASE_URL=postgres://argos:argos@localhost:5432/argos_test?sslmode=disable` (la BD existe en el Postgres local). `-race` no corre local (falta gcc); lo cubre el CI.
