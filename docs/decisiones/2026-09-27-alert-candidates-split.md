# O1: `alert_candidates` por rama de tipo + `dead_at` real (2026-09-27, migración 00035)

## O1 — optimizar `alert_candidates`
Medido con 1000 hosts y 8 reglas (una por tipo): la vista pasó de ~0,25–0,5 s por pasada a **13–64 ms** con datos representativos de una flota real (mismo SO repetido entre hosts). Un caso extremo fabricado a propósito (cada host con una combinación de SO distinta) sigue en ~250–380 ms; no se persiguió porque no representa una flota real.

- **Antes:** un solo `LATERAL` calculaba `os_eol`/parches/inventario para cada fila, aunque no hubiera ninguna regla de ese tipo.
- **Ahora:** `UNION ALL` con una rama por `kind` (metric, host_status, no_data, service_check, exposed_port, pending_updates, os_eol); cada regla solo paga el JOIN de su propio tipo. `exposed_port` parte de las claves `added` del host (normalmente 0) antes de tocar el inventario completo; `os_eol` calcula la clave de fin de vida una vez por combinación distinta de (os, os_name, os_version), no una vez por host.
- **Test de equivalencia** (`alert_candidates_equivalence_test.go`, contra `testdata/alert_candidates_legacy.sql`, copia literal de la vista de la migración 00029): compara clave, breach, clear, detail y valor entre la vista nueva y la vieja dentro de la misma transacción (evita ruido de `now()`), con los 10 tipos de regla sembrados. Queda como referencia permanente de regresión.
- **Deuda:** no hay benchmark de tiempo automatizado en Go (solo medición manual con `EXPLAIN`); un gate de rendimiento en CI necesitaría decidir el margen y la máquina de referencia.

## Entregas `dead` con fecha real
`notification_deliveries.dead_at` (misma migración; NULL salvo `status='dead'`). Se fija en el momento real en que una entrega muere (`MarkDeliveriesFailed`); las entregas muertas antes de esta migración llevan `dead_at = created_at` (backfill simple, sin fecha real: límite conocido si se audita `/system/status` con datos viejos). `SystemStats` ya filtra `dead_24h` por `dead_at`, no por `created_at`.
