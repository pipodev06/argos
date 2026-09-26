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

**Opciones:** (la 1 se implementó el mismo día, ver abajo; la 2 y la 3 no)
1. Que el rango **7d** lea `metrics_hourly` (mismo paso de 1 h; ~170 filas en vez de 20 k) más el crudo de la hora en curso.
2. Paso de mantenimiento que haga `CLUSTER` de las particiones diarias ya cerradas (fuera de la hora pico, con `lock_timeout`).
3. Índice cubridor (`INCLUDE`) para permitir índices-solo: duplica el espacio del índice.
La 1 es la más barata y con más ganancia; la 2 protege también el rango de 24 h.

## Sin medir todavía
Mantenimiento/rollup con 1000 hosts (ciclo completo), evaluador de alertas sobre los 1000 hosts con reglas (medido antes con el arnés de tests: 1,27 s la primera vez y 746 ms en régimen, con intervalo de 15 s), 7 días y 30 días de crudo completos, carga sostenida, y un server Linux dedicado con la BD fría. el `/summary` con 8 concurrentes (p95 92 ms) merece una segunda pasada.

## Implementado: 7d desde resumenes por hora (opción 1)
- `Store.MetricSeriesMixed` (solo paso de 1 h): lee `metrics_hourly` de las horas cerradas y el crudo solo desde `max(hour)+1 h` del host (por lo común la hora en curso; más si el mantenimiento va atrasado; sin resúmenes, todo del crudo). Los buckets son los mismos del crudo (`date_bin` de 1 h a la epoch). Único cambio visible: el primer bucket parcial (la hora que contiene `from`) ya no sale, igual que en 30d/90d/1y. `GET /hosts/{id}/metrics?range=7d` usa esta fuente; 1h/6h/24h siguen en crudo.
- Medido en la BD sintética: **7d p50 50 ms → 2,8 ms (p95 4,1 ms)** y ya no depende del volumen de crudo (con 7 días completos habría sido ~120 ms). Tests: mezcla horaria + cola cruda (el crudo de una hora ya resumida no cuenta), host nuevo = idéntico al crudo, casos borde (vacío, inexistente, paso inválido) y el handler verifica qué fuente usa cada rango. La equivalencia crudo/horario en promedios ya la cubría `retention/rollup_test.go` (cpu 1e-3, mem 1 MB, rx 2 %).
- La BD `argos_perf` se borró tras medir; se regenera con `bash scripts/perf/run.sh setup 1000 3`.

## Cómo se controla el riesgo de la caché fría (decidido tras el 7d desde resúmenes)
- **Ya no hay nada que agrupar:** con 7d leyendo `metrics_hourly`, el crudo de días anteriores a ayer solo lo lee el mantenimiento (rollup/borrado). Solo las gráficas de 1 h, 6 h y 24 h tocan el crudo, y esas leen **hoy y ayer**: las particiones más calientes (se escriben sin parar, viven en caché). Un `CLUSTER` de particiones *cerradas* casi no ayudaría y bloquearía la lectura de ayer hasta 54 s: **descartado** (la partición de hoy, abierta, no se puede agrupar).
- **Qué queda expuesto:** el primer acceso a una gráfica de 24 h tras reiniciar Postgres/el server (caché vacía) o con menos RAM que las ~2 últimas particiones (+ índice): ~2880 lecturas de página por host, rápidas en SSD y muy lentas en disco mecánico.
- **Controles:** (1) *Dimensionar la RAM:* las últimas 2 particiones con índice ocupan ~1 GB por 1000 hosts a 30 s (medido: 8,6 M filas de crudo ≈ 1,4 GB con su índice); el server necesita ese margen libre para la caché del SO además de `shared_buffers` (subirlo de los 128 MB por defecto a ~25 % de la RAM). (2) *Detectarlo:* el log de solicitudes ahora marca Warn cualquier solicitud que tarde `>= 1 s` (`journalctl -u argos-server | grep '"level":"WARN"' | grep '"ms"'`). (3) *Si aun así aparece:* `pg_prewarm` de las particiones de hoy y ayer al arrancar (extensión contrib, opcional), y más allá de ~5000 hosts, TimescaleDB con compresión por `host_id` (ya anotado en deuda).
- **No cubierto:** un disco mecánico con menos RAM que el dato caliente seguirá lento en el primer acceso; no es un objetivo de despliegue.

## S1/S2: la suite del server (2026-09-25)
- **Medido** (`go test -json`): 204 s en total, 100 % serializado (los tres paquetes con BD comparten `argos_test` bajo un lock advisory, así que el tiempo es la suma: store 79 s, httpapi 65 s, retention 56 s). Distribución: 258 tests entre 0,1 y 0,5 s = 86 s; 76 entre 0,5 y 1 s = 48 s; 23 entre 1 y 5 s = 36 s; 1 de 31 s.
- **Causa 1 (~31 s):** `TestRuntimeRoleSplit` creaba y borraba una BD por corrida; `DROP DATABASE` fuerza un checkpoint inmediato que con los datos sucios de los tests previos tardaba 30+ s. Ahora la BD y los roles `argos_t_*` se conservan y solo se recrea el esquema (1 s).
- **Causa 2 (~110 s):** cada test limpiaba con `TRUNCATE ... CASCADE` de 16 tablas = **183 ms** fijos (arrastra ~180 relaciones con particiones y crea archivos nuevos; ni `synchronous_commit=off` ni menos tablas lo bajaban). Ahora `internal/testdb.ResetData` borra por filas (**~12 ms**), con `TRUNCATE audit_log` aparte (el trigger append-only bloquea DELETE) y el TRUNCATE completo como respaldo si alguna FK nueva rompe el orden. `ARGOS_TEST_VERIFY_RESET=1` comprueba que no quedó ninguna fila fuera de `os_eol`/`goose_db_version` (verificado sobre toda la suite de store y httpapi).
- **Resultado:** **204 s → 73 s** (store 25, httpapi 46, retention 71 con la espera del lock), suma de tests ~69 s. Lo que queda son los tests de retención/migraciones que recrean el esquema entero (~0,8–1 s cada uno).
