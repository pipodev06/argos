# E3: empaquetado con volumen, `disk_samples` en chunks, `--race` (2026-09-27)

## Empaquetado con volumen (backfill real)
Escenario: `scripts/perf/run.sh setup 1000 10` — 1000 hosts, 10 días de historial nunca empaquetado (28,8 M filas de `metrics`, 5,76 M de `disk_samples`, 6,1 GB), server de prueba aparte con `ARGOS_METRICS_MAINTENANCE_INTERVAL=5s` para observar varios ciclos rápido.

- Cada partición diaria (1000 host-días) tardó **~1 min 45 s** en empaquetarse por completo (métricas: 2,04–2,88 M muestras; disco: 0,4–0,58 M muestras), en una máquina que a la vez tenía el server de desarrollo activo sobre la misma instancia de Postgres — en un server dedicado debería ser más rápido.
- `defaultPackBudget = 1` (una partición por ciclo) es seguro con el intervalo por defecto de 5 min: el empaquetado real (~2 min) deja margen amplio antes del siguiente ciclo. Con un backlog grande (muchos días sin empaquetar) el ritmo de recuperación es de una partición por ciclo real, no más rápido aunque el trabajo en sí sea corto — es la limitación deliberada del diseño (no monopolizar la BD), confirmada aquí con datos reales.
- Las particiones crudas se **borraron solas** tras empaquetarse y verificarse (sin esperar la retención completa): la BD bajó de 6118 MB a 5163 MB al liberar 2 días de crudo.
- Tamaño real del chunk: metrics ≈ **20 070 bytes/host-día** (coincide con la estimación de diseño, ~23 KB); disk ≈ **1417 bytes/host-día** (2 montajes, ~576 muestras/día) ≈ 2,46 B/muestra.
- **Hallazgo real, no simulado:** con `ARGOS_METRICS_MAINTENANCE_INTERVAL=5s` (usado solo para acelerar la prueba, no es un valor de producción) el auto-monitoreo de B6 marcó el check `maintenance` en `crit` durante cada ciclo largo ("aún sin ciclo correcto desde el arranque"), porque el umbral de warn/crit (2/5 intervalos) se calcula sobre el intervalo configurado. Es el comportamiento esperado del check, no un bug: con el intervalo de producción (5 min) un ciclo de ~2 min nunca dispara la alerta. Documentado como aviso: si alguien baja `ARGOS_METRICS_MAINTENANCE_INTERVAL` mucho en producción, el check de B6 puede dar falsos positivos durante un backfill grande.
- Sin degradación observada entre el primer y el segundo ciclo (tiempos similares); sin errores ni fugas evidentes.

## `disk_samples` en chunks
Migración `00036_disk_chunks.sql` (`disk_chunks(host_id, day, mount_count, n, codec, data, packed_at)`), códec propio `internal/chunk/disk.go` (multiplexa montajes por host-día; delta-de-delta de `ts` + delta/zigzag/varint por columna, sin bitmap de presencia porque `used_mb`/`total_mb` nunca son NULL; flate 9 + CRC32; reusa los helpers de varint que se extrajeron de `chunk.go`).

- **Ventana caliente fija de 8 días** (`retention.diskHotDays`, constante interna, NO configurable): `disk_samples` nunca tuvo retención por host/grupo ni resumen por hora, así que no se le añadió; `ARGOS_DISK_RETENTION_DAYS` sigue siendo la única perilla, ahora repartida entre crudo (8 días) y `disk_chunks` (el resto).
- Lectura: `Store.DiskDetailRange` mezcla crudo + chunks igual que `DetailRange` para métricas; el historial y el detalle del host no cambiaron de contrato.
- Purga de host/grupo borra también `disk_chunks`.
- Verificado con datos reales (arriba): compresión ≈ 2,46 bytes/muestra.

## `check-go.sh --race`
Se reprodujo el intento con gcc ya instalado en la máquina: **salió en verde** (269 s, 10 paquetes). No se reprodujo el fallo suelto anotado como deuda. Se deja la entrada de deuda pero rebajada: "no reproducido en el intento más reciente; si vuelve a pasar, guardar el log completo antes de reintentar".
