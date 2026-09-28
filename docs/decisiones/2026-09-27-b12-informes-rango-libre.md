# B12: informes por rango libre y mes calendario (2026-09-27)

## Diseño
`parseReportParams` (compartida por los 5 informes: `availability`, `availability.csv`, `service-checks`, `service-checks.csv`, `alerts.csv`) admite 3 formas de pedir el periodo, sin combinarlas:
- **`range=7d|30d|90d`** (como antes, sin cambios).
- **`from`+`to`** (RFC3339, rango libre): `from < to`, `to` no puede ser futuro, y el rango no puede pasar de **91 días** (`maxReportRangeDays`) — cubre cualquier mes calendario con margen; `host_events` solo garantiza 400 días de historial, así que tampoco tiene sentido un rango mucho mayor sin repensar esa retención.
- **`month=AAAA-MM`** (mes calendario completo, en UTC): atajo para no calcular `from`/`to` a mano; no admite un mes futuro. Pensado sobre todo para uso directo de la API/CSV, no tiene control propio en la web (ver abajo).

Combinar dos de las tres formas a la vez es 400 `invalid_body`. La respuesta y el nombre del CSV usan `"range": "custom"` (from/to) o el propio `"AAAA-MM"` (month) donde antes solo salían `"7d"/"30d"/"90d"`.

## Bug real corregido de una (no aplazado, según la excepción de "afecta el flujo principal"): ninguno esta vez
No se encontró ninguno nuevo en este lote (si acaso, ver el de B7 en el lote anterior). Sí se agregó un test de contrato (`TestReportFreeRange`) porque es lógica de validación central compartida por 5 endpoints — un error ahí rompe todos los informes a la vez.

## Web
`ReportsPage`: el selector de rango gana "Personalizado", que muestra dos campos de fecha y hora (mismo patrón que "Historial por fecha" de un host). Mientras falte alguna fecha, no se consulta el informe (se avisa en vez de mandar una petición inválida) y los botones de exportar CSV quedan deshabilitados. Sin control propio para `month` en la web (se decidió no construir un selector de mes aparte cuando el rango libre ya cubre el mismo caso a mano) — queda disponible solo para quien arme la URL/CSV directamente.

## Deuda
Sin test web de los campos de fecha personalizados ni de que el CSV/informe no se dispare con el rango incompleto (aplazado, ver `tests-pendientes.md`).
