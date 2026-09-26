# Rendimiento del server con 1000 hosts (medido el 2026-09-25, plan P1a)

**Cómo se midió:** `scripts/perf/` (`run.sh setup 1000 3` siembra la BD desechable `argos_perf` con 1000 hosts, 8,6 M muestras crudas (30 s × 3 días), 1,7 M de disco, 721 k resúmenes por hora y parches/inventario; `run.sh start` levanta un server en :18082; `run.sh load` corre `loadgen`, que mide p50/p95/p99 con 1 y 8 clientes y simula agentes reales). Nunca toca la BD real.
**Máquina:** laptop i7-1165G7 (4 núcleos/8 hilos), 15,7 GB RAM, Windows, Postgres 18 con `shared_buffers=128MB` (por defecto), y el server, Postgres y el generador de carga **en el mismo equipo** (los números de un server dedicado deberían ser mejores). Datos calientes (repetidos); los fríos dependen del disco.

## Lectura (1 cliente, p50 / p95)
| Endpoint | p50 | p95 |
|---|---|---|
| `/health` | 0,8 ms | 1,5 ms |
| `/summary` | 5,8 ms | 8,4 ms |
| `/hosts?limit=15` (sin orden / cpu / mem / disco / severidad) | 5–9 ms | 7–15 ms |
| `/hosts?limit=100` | 10 ms | 19 ms |
| `/hosts?q=…` / `status=down` | 3 ms | 4 ms |
| `/alerts`, `/alerts/summary`, `/alert-rules`, `/groups` | 1–2 ms | 2–3 ms |
| `/updates` (flota, 15 filas; con filtro de seguridad) | 36 ms | 50–63 ms |
| `/reports/availability?range=7d` (281 KB) / `30d&by=group` / CSV | 20–25 ms | 29–40 ms |
| `/hosts/{id}/metrics?range=24h` (2880 muestras crudas) | 17–22 ms | 28–30 ms |
| `/hosts/{id}/metrics?range=7d` (**con solo 3 días de datos**) | 46–56 ms | 70–106 ms |
| `/hosts/{id}/metrics?range=30d` (resúmenes por hora) | 5,5 ms | 7,5 ms |
| detalle de host, eventos, inventario, parches del host | 1,5–7 ms | 3–8 ms |

Con 8 clientes concurrentes nada se degrada de forma seria (la lista sube a ~700–900 req/s; `/summary` tuvo un p95 de 92 ms con 8 a la vez). Los informes dan 429 con 8 simultáneos **por diseño** (tope de informes concurrentes).
**Nada pasó de 200 ms en caliente.** Con 7 días completos el rango 7d valdría ~2,3× lo medido (≈120 ms p50).

## Ingest (`POST /agent/metrics`)
| Carga | req/s | p50 | p95 | p99 | max | errores |
|---|---|---|---|---|---|---|
| 1000 agentes a ritmo real (cada 30 s) | 22 (arranque escalonado) | 4,7 ms | 8,1 ms | 9 ms | 32 ms | 0 |
| 1000 agentes ×10 (cada 3 s) | **309** | 5,7 ms | 12,2 ms | 25 ms | 181 ms | 0 |

309 req/s equivalen a ~9000 agentes a 30 s. No se probó una carga sostenida de horas (WAL, mantenimiento, autovacuum).

## Hallazgo: la disposición física de `metrics` (riesgo con caché fría)
Las filas se escriben por tiempo, así que las de un host quedan **una por página** (plan real: `Heap Blocks: exact=2880` para 2880 filas). Con caché caliente no se nota (20–50 ms), pero con la BD más grande que la RAM cada gráfica sería ~2880 lecturas aleatorias por día pedido. Experimento: `CLUSTER metrics_pYYYYMMDD USING …_pkey` en 4 particiones cerradas (27–54 s cada una, 2,9 M filas, bloqueo exclusivo) baja las páginas leídas de la consulta de 7d de 8700 a ~500 (17×) y su tiempo de BD a ~20 ms; la latencia caliente casi no cambia. Además: `Planning Time` ~6,5 ms por la consulta sobre una tabla particionada.

**Opciones (no implementadas; decidir):**
1. Que el rango **7d** lea `metrics_hourly` (mismo paso de 1 h; ~170 filas en vez de 20 k) más el crudo de la hora en curso.
2. Paso de mantenimiento que haga `CLUSTER` de las particiones diarias ya cerradas (fuera de la hora pico, con `lock_timeout`).
3. Índice cubridor (`INCLUDE`) para permitir índices-solo: duplica el espacio del índice.
La 1 es la más barata y con más ganancia; la 2 protege también el rango de 24 h.

## Sin medir todavía
Mantenimiento/rollup con 1000 hosts (ciclo completo), evaluador de alertas sobre los 1000 hosts con reglas (medido antes con el arnés de tests: 1,27 s la primera vez y 746 ms en régimen, con intervalo de 15 s), 7 días y 30 días de crudo completos, carga sostenida, y un server Linux dedicado con la BD fría. el `/summary` con 8 concurrentes (p95 92 ms) merece una segunda pasada.
