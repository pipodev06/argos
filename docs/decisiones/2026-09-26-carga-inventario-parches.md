# Carga de inventario y parches, soak y caché fría (2026-09-26, L1–L4)

Herramientas: `scripts/perf/loadgen` (ahora con `-inventory`, `-updates`, `-metrics=false`, `-inv-warm`, `-inv-change`, `-inv-stale`; replica la cadencia real del agente: ports 1 min, services 10 min, processes 5 min, parches 6 h ±20 %; latidos 6 h/6 h/30 min/24 h) y `scripts/perf/run.sh`. Máquina y método como en `2026-09-25-rendimiento.md` (portátil compartido con el usuario, Postgres con `shared_buffers=128MB`, BD sintética de 1000 hosts × 3 días). Corridas ×10 y ×100 con el server en modo Debug.

## L2 — ingest con 1000 agentes (p50 / p95 / p99)
| Escenario | Petición | p50 | p95 | p99 |
|---|---|---|---|---|
| ×1 en régimen (5 min) | processes completo | 6,8 ms | 10,9 ms | 70 ms |
| | ports/services/parches completos | 8–21 ms | 13–28 ms | ≤32 ms |
| | latidos | 3,8–6,4 ms | ≤6,8 ms | – |
| | métricas (30 req/s) | 8,1 ms | 47 ms | 52 ms |
| ×10 (incl. 325 req/s de métricas) | processes completo (10 req/s) | 6,1 ms | 176 ms | 267 ms |
| | métricas | 17,5 ms | 108 ms | 151 ms |
| ×100 solo inventario | processes completo (100 req/s) | 5,6 ms | 33 ms | 711 ms |
| **Arranque en frío** (3000 envíos completos en ~5 s) | ports/processes/services completos | ~1,2 s | ~1,33 s | ~1,36 s |
| | métricas durante el arranque | 8,6 ms | 270 ms | 2,3 s |

Sin 5xx ni 413/429 en inventario (el límite de 20/min por host no se alcanzó ni a ×100). El 409 masivo de ×100 es un artefacto del simulador (`-inv-warm`). La sobrecarga ×100 *con* métricas (3300 req/s) dio 671 k respuestas 429: es una prueba de límites, no un ritmo real.
**Crecimiento:** `inventory_current` 9 MB por 3000 filas; `updates_history` 451 B por foto. `inventory_changes` (223 MB en la prueba) no es representativo: el simulador reemplaza todos los items en cada cambio; un host real cambia 1–2 items. Tiene purga por lote (retención 90 d).

## Evaluador de alertas (1000 hosts)
| Situación | Ciclo |
|---|---|
| Sin reglas propias | 72 ms |
| 8 reglas (mem, load, disco, sin datos, parches, reinicio, os_eol, puertos), 1000 alertas de puertos + 300 de parches | 0,7–0,95 s con pocas abiertas; 1,4–1,7 s con ~1300 abiertas; ~2 s bajo ingest; 3,7 s en la soak |
Con el intervalo de 15 s ocupa entre el 5 % y el 25 %. **Cuello:** `SELECT count(*) FROM alert_candidates WHERE breach` = 420–465 ms (planning 37–45 ms); las dos subconsultas LATERAL de la vista (`os_eol_key`+`host_info` y los días de parches) se ejecutan **por fila** (≈375 de los ~420 ms) aunque las reglas de ese tipo estén deshabilitadas. Escala con hosts × reglas (~2 s por pasada con ~5000 hosts). Candidato de optimización; no se tocó.

## L3 — soak de 30 min (1000 agentes, métricas + inventario + parches ×1)
Cero 5xx y cero errores de red. Métricas p50 21,8 ms / p95 69 ms / p99 80 ms (59 944 peticiones); envíos completos 7–15 ms p50. **RSS del server 34,8 → 38,3 MB (sin fuga)**, CPU ~6 % de un núcleo, 11 conexiones fijas, BD +33 MB, autovacuum al día (n_dead_tup bajo), mantenimiento sin errores. Hallazgo no reproducido: entre el primer y el último tercio la p50 de métricas subió de 8,9 a 33,5 ms y el ciclo del evaluador se duplicó; una corrida de 12 min con el mismo perfil dio 4,4/4,4 ms. Hipótesis: contención de la máquina (antivirus/Windows); repetir la soak en un equipo sin uso.

## L4 — caché fría (clúster temporal, PG 18, ~2 GB)
Con `shared_buffers=16MB` la caché de Postgres no sirve (una gráfica de 24 h toca ~3000 páginas): caliente y tras reinicio dan lo mismo (79–86 ms). Con 1 GB: tras reinicio 102 ms (2954 lecturas), caliente 53 ms (0 lecturas), con `pg_prewarm` de hoy y ayer (98 k bloques, ~770 MB, 1,28 s) 62 ms (−40 %). **No se pudo vaciar la caché del SO** (sin admin en Windows): en disco frío es estimación (~0,3–0,6 s en SSD, ~25 s en HDD). Conclusión: con SSD y caché del SO no hace falta `pg_prewarm`; opcional `shared_preload_libraries='pg_prewarm'` con autoprewarm (sin probar) y solo aporta con `shared_buffers` ≥ ~0,7 GB por 1000 hosts.

## Derivado (ver `plan-tareas.md`, P6)
Optimizar la vista `alert_candidates`; jitter mayor del envío completo al arrancar el agente (la ráfaga de 3000 completos atrasa las métricas hasta 2,3 s); muestrear las líneas de log de las respuestas 429 (un flood deja una línea WARN por solicitud); variable de nivel de log (el server es Info fijo, el log Debug "ciclo de alertas" no se ve); repetir la soak sin uso concurrente.
