# O4: soak de 30 min repetida (2026-09-27)

Mismo escenario que `2026-09-26-carga-inventario-parches.md` (L3): 1000 agentes simulados, métricas + inventario + parches, `scripts/perf/run.sh` (BD `argos_perf`, 1000×3 días). Máquina compartida con el resto de esta sesión (2 agentes de código corriendo en paralelo durante buena parte de la corrida) — **no** fue una máquina sin uso concurrente, así que esto es una repetición aproximada, no la prueba limpia que O4 pedía.

## Resultado
- **0 errores 5xx, 0 líneas ERROR** en 30 min. Los únicos 429 fueron en los endpoints de informes bajo concurrencia 8 (cupo propio de exportación, esperado, no es un hallazgo).
- **RSS 28–38 MB** durante toda la corrida, sin tendencia al alza (fin en 30,1 MB) — **sin fuga**, igual que la corrida original.
- **Métricas**: primer tercio p50 4 ms / p95 10,8 ms / p99 185,6 ms / max 695,4 ms; último tercio p50 7,5 ms / p95 25,3 ms / **p99 30,2 ms / max 58,8 ms**. El p50/p95 subió un poco (esperable con más filas acumuladas), pero el p99 y el máximo **mejoraron** respecto al primer tercio — lo opuesto al hallazgo no reproducido de la corrida original (ahí la cola empeoraba con el tiempo). Compatible con que aquello fuera contención puntual de la máquina (antivirus/Windows) y no una degradación real del server.
- Los reportes de puertos/servicios/procesos/parches (envío completo) tuvieron colas largas en el primer tercio (p99 hasta 221 ms) que casi desaparecieron en el último tercio — consistente con el jitter de arranque (O2) haciendo su trabajo: la ráfaga inicial se dispersa y el resto de la corrida queda liviana.

## Pendiente real
Repetir esto en un equipo sin uso concurrente sigue siendo la única forma de confirmar limpiamente que no hay degradación por carga sostenida; esta corrida es una segunda señal a favor de que no la hay, no una prueba definitiva.
