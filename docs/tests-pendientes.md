# Tests pendientes (deuda de pruebas)

Mientras se ataca el core de negocio (B7, B12, …) no se escriben tests nuevos por cada feature — verificar contra Postgres real es lo que más tiempo/tokens consume. Cada punto de abajo es un test que faltó a propósito; se cierran en una pasada de tests dedicada, no uno por uno. Excepción: un bug que afecta el flujo principal SÍ lleva su test al corregirlo (no aplaza).

## Pendientes
- ~~**B17, export de grupo:** sin test que fuerce el tope `maxGroupExportHosts=5000`.~~ **Cerrado 2026-09-27** (`group_export_test.go`, `TestExportGroupHostsCap`).
- ~~**B16, alta masiva:** falta un test end-to-end del flujo completo agente→enroll con `hostname` real.~~ **Cerrado 2026-09-27** (`bulk_enroll_api_test.go`, `TestBulkEnrollHttpFlow`).
- ~~**B14/B15, re-enroll:** sin test del efecto sobre alertas abiertas.~~ **Cerrado 2026-09-27** (`quota_test.go`, `TestReenrollAlertLifecycle`) — **hallazgo real, no un bug**: re-enrolar un host caído NO resuelve su alerta de inmediato. El host queda `pending` (no `up`) hasta el primer reporte real del agente; `host_status` solo mira hosts `up`/`down` (o `offline` si `include_offline`), así que un host `pending` no entra como candidato y la alerta sigue `open` hasta ese primer reporte. Es el comportamiento correcto (no debería "recuperarse" solo porque se generó un token), pero antes no estaba confirmado con un test.

- ~~**B7, cuotas por cliente:** sin test end-to-end de bloqueo.~~ **Cerrado 2026-09-27** (`quota_test.go`: `TestGroupQuotaHosts`, `TestGroupQuotaDevices`, `TestGroupQuotaUsers`, `TestGroupQuotaChecksRulesChannels`, `TestGroupQuotaOnMove`).
- **B7 web:** sin test de `GroupFormDialog` para los campos de cuota (validación, payload armado) ni de `useGroupUsage`.

- **B12, informes por rango libre:** sin test web de los campos "Desde"/"Hasta" personalizados ni de que el informe/CSV no se dispare con el rango incompleto.

(Vacío a partir de aquí: se va llenando según avance el core sin tests.)
