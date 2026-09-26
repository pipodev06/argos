# Plan de tareas para matar la deuda (creado 2026-09-25)
Una tarea = un commit (o pocos) con su prueba. Marca `[x]` al cerrar y mueve el detalle a `deuda-tecnica.md`/`decisiones/`. Orden = prioridad. Cada tarea trae su "hecho cuando".

## P0 — Antes/al desplegar en el server Linux
- [ ] **D1 Primer despliegue real** con `deploy/` (ver `deploy/README.md`): instalar, `systemctl status`, `systemd-analyze security argos-server`, respaldo (`systemctl start argos-backup.service`), restaurar el dump en una BD de prueba. *Hecho cuando:* login por HTTPS desde otro PC y un agente enrolado contra ese server; ajustes anotados.
- [x] **D2 `/health` con chequeo de BD** (2026-09-25) (sin auth, sin datos internos) para monitorear el propio server (y uso en Caddy/uptime). *Hecho cuando:* 200 con BD viva, 503 si Postgres cae; test.
- [x] **D3 Request logging mínimo** (2026-09-25) (método, ruta sin query, estado, ms, sin cabeceras ni cuerpos). *Hecho cuando:* visible en journalctl; un test verifica que no se loguean tokens/cookies.
- [x] **D4 Separar roles de BD** (2026-09-25) (migrador vs runtime; cierra el hueco del trigger append-only de `audit_log`). *Hecho cuando:* el unit corre con rol sin DDL; `install-server.sh` crea ambos; test de que el runtime no puede `DROP TRIGGER`.

## P1a — Rendimiento del server en runtime (nunca medido a escala, 2026-09-25)
- [x] **P1 Sembrar 1000 hosts** (2026-09-25, `scripts/perf/`) con historial (script en `scripts/`, solo contra una BD `*_test` o de humo, nunca la real). *Hecho cuando:* `bash scripts/seed-load.sh` deja la BD lista y repetible.
- [x] **P2 Medir p50/p95** (2026-09-25, `decisiones/2026-09-25-rendimiento.md`) de: `GET /hosts` (orden por cpu/mem/disco, filtros, cursor), `/summary`, `/hosts/{id}/metrics` (24h/7d/30d), `/alerts`, `/updates`, informes + CSV, e **ingest** (`POST /agent/metrics` e inventario) con N agentes concurrentes. *Hecho cuando:* tabla de resultados en `decisiones/` con la máquina usada.
- [ ] **P3 `EXPLAIN (ANALYZE, BUFFERS)`** (nada pasa de 200 ms en caliente; **7d desde `metrics_hourly` hecho**: 50 → 2,8 ms; el `CLUSTER` de particiones cerradas se **descartó** (ver "Cómo se controla el riesgo de la caché fría"); queda medir el ciclo de mantenimiento y el evaluador con 1000 hosts) de lo que pase de 200 ms y corregir (índices, consultas, rollups). *Hecho cuando:* cada consulta lenta mejorada o justificada, y un test de humo de tiempo (con margen) para no regresar.
- [ ] **P4 Tope y avisos**: cuántos hosts aguanta un server de cierta RAM/CPU; documentarlo en `deploy/README.md`.

## P1 — Suite del server (lenta: ~190–340 s)
- [x] **S1 Medir** (2026-09-25, `decisiones/2026-09-25-rendimiento.md`): `go test -json` → top 20 tests/paquetes más lentos (sospechas: hash argon2, `TRUNCATE` por test, `sleep`s). *Hecho cuando:* tabla de tiempos en `decisiones/`.
- [x] **S2 Recortar** (204 s → 73 s: `testdb.ResetData` y sin DROP DATABASE por corrida) lo medido: params de argon2 baratos en tests, esquema compartido con `t.Parallel()` donde no haya estado global, quitar esperas fijas. *Hecho cuando:* `check-go.sh server` < 90 s sin perder cobertura (comparar `-cover`).
- [x] **S3 `-race` en local** (2026-09-25: `bash scripts/check-go.sh server --race`; suite del server con `-race` en 2 min 43 s: httpapi 151 s, store 114 s, retention 62 s; sin carreras) (o job propio en CI con caché): hoy solo corre en CI Linux. *Hecho cuando:* documentado el comando y el tiempo.
- [x] **S4 Tests dependientes de la hora** (2026-09-26: revisados los 110 usos de `time.Now()` en tests; los de límite de hora/día ya estaban corregidos; el CI ahora corre server y agent también con `TZ=Pacific/Kiritimati` (UTC+14) y `TZ=Etc/GMT+12` (UTC-12), job `go-tz`: los 4 verdes) : buscar `Now()±N` sin reloj inyectado (fallaron 2 por hora/zona). *Hecho cuando:* grep limpio o justificado.

## P2 — Reglas de alerta de parches y alertas en general
- [ ] **R1 Prueba E2E real** de `pending_updates`, `reboot_pending` y `os_eol` con los 2 hosts (crear regla en la UI, forzar la condición, ver alerta, ver notificación en el webhook). *Hecho cuando:* checklist ejecutado y hallazgos corregidos.
- [x] **R2 Tests Vitest de formularios** (2026-09-26; 267 tests; hallazgo: ventanas de mantenimiento en curso no se podían alargar, corregido) (`ruleForm`, `checkForm`, `maintenanceForm`, `alertQuery`): hoy sin cobertura. *Hecho cuando:* casos válidos/inválidos y de borde por tipo de regla.
- [x] **R3 Deudas del motor** (2026-09-26; pico y severidad de `os_eol` corregidos; 2 puntos descartados con razón): `peak_value` sin sentido en `os_eol`; severidad de `os_eol` no escala si la alerta ya estaba abierta; `duration_seconds` cuenta desde `security_pending_since`; dedup de auditoría en memoria. *Hecho cuando:* cada punto corregido o descartado con razón escrita.
- [x] **R4 Reverificar EOL** (2026-09-26, migración 00032; Windows Server 2025 y macOS Big Sur corregidos) de Windows 10/Server (Microsoft Learn) y decidir qué hacer con macOS (`estimate`); fecha y fuente en cada fila. *Hecho cuando:* `source='official'` con enlace en la migración.

## P3 — Mejoras de producto (ideas; una por vez, con diseño corto antes)
- [ ] **I1 Windows con servicio privilegiado**: `root_home` conoce el dueño de 41/103 puertos (sin privilegios) vs 23/23 en Ubuntu (elevated). Instalar con `--privileged` y comparar. *Hecho cuando:* medido y documentado.
- [ ] **I2 Parches: más que "pendientes"** (pregunta del usuario). Hoy solo Windows Update `Type='Software'` (drivers excluidos) y gestores Linux; el antivirus solo entra si Defender publica firmas por Windows Update, sin distinguirlo. Propuestas: (a) módulo `security` con estado de Defender (`Get-MpComputerStatus`: protección en tiempo real, edad de firmas), (b) drivers pendientes de Windows Update (opt-in), (c) Linux: kernel en uso vs instalado, `needrestart`. *Hecho cuando:* el usuario elige y hay diseño en `decisiones/`.
- [ ] **I3 Disco físico en la lista de Hosts** (columna o tooltip). 
- [ ] **I4 SNMP**: contadores de interfaz, CPU y almacenamiento; gráfica.
- [ ] **I5 Gráfica del historial de parches** y filtro booleano/`≤` en `FilterBar`.

## P4 — Aplazado por decisión del usuario
SMTP · dominio/VPS público (D1 cubre LAN) · firma de binarios y auto-actualización · buffer offline persistente del agente.
