# Tests pendientes (deuda de pruebas)

Mientras se ataca el core de negocio (B7, B12, …) no se escriben tests nuevos por cada feature — verificar contra Postgres real es lo que más tiempo/tokens consume. Cada punto de abajo es un test que faltó a propósito; se cierran en una pasada de tests dedicada, no uno por uno. Excepción: un bug que afecta el flujo principal SÍ lleva su test al corregirlo (no aplaza).

## Pendientes
- **B17, export de grupo:** sin test que fuerce el tope `maxGroupExportHosts=5000` (comportamiento hoy: corta en silencio, sin 422).
- **B16, alta masiva:** cobertura HTTP de los 400 (max_uses fuera de rango, sin group_id) sí quedó escrita; falta un test end-to-end del flujo completo agente→enroll con `hostname` real cuando el token es de grupo (hoy solo probado a nivel store + un test de que `enroll.Do` manda el hostname, no el viaje completo HTTP con un servidor real).
- **B14/B15, re-enroll:** sin test de que re-enrolar un host reactiva también sus alertas abiertas relacionadas con "host caído" (se probó que el ingest deja de dar 401, no el efecto sobre alertas existentes).

(Vacío a partir de aquí: se va llenando según avance el core sin tests.)
