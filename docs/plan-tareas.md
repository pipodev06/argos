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
- [x] **R1 Prueba E2E real** (2026-09-26, sobre una COPIA de la BD con receptor HTTPS local; ver `decisiones/2026-09-25-parches.md`: las 3 reglas abren con el detalle correcto —incl. Windows 11 25H2, fin 2027-10-12, 381 días—, se resuelven al recuperar y el webhook llega agrupado en 1 mensaje; hallazgo menor: el texto de una alerta abierta usa el detalle vigente al enviar, no el de la apertura; UI no revisada en navegador) de `pending_updates`, `reboot_pending` y `os_eol` con los 2 hosts (crear regla en la UI, forzar la condición, ver alerta, ver notificación en el webhook). *Hecho cuando:* checklist ejecutado y hallazgos corregidos.
- [x] **R2 Tests Vitest de formularios** (2026-09-26; 267 tests; hallazgo: ventanas de mantenimiento en curso no se podían alargar, corregido) (`ruleForm`, `checkForm`, `maintenanceForm`, `alertQuery`): hoy sin cobertura. *Hecho cuando:* casos válidos/inválidos y de borde por tipo de regla.
- [x] **R3 Deudas del motor** (2026-09-26; pico y severidad de `os_eol` corregidos; 2 puntos descartados con razón): `peak_value` sin sentido en `os_eol`; severidad de `os_eol` no escala si la alerta ya estaba abierta; `duration_seconds` cuenta desde `security_pending_since`; dedup de auditoría en memoria. *Hecho cuando:* cada punto corregido o descartado con razón escrita.
- [x] **R4 Reverificar EOL** (2026-09-26, migración 00032; Windows Server 2025 y macOS Big Sur corregidos) de Windows 10/Server (Microsoft Learn) y decidir qué hacer con macOS (`estimate`); fecha y fuente en cada fila. *Hecho cuando:* `source='official'` con enlace en la migración.

## P3 — Mejoras de producto (ideas; una por vez, con diseño corto antes)
- [ ] **I1 Windows con servicio privilegiado**: `root_home` conoce el dueño de 41/103 puertos (sin privilegios) vs 23/23 en Ubuntu (elevated). Instalar con `--privileged` y comparar. *Hecho cuando:* medido y documentado.
- [ ] **I2 Parches: más que "pendientes"** (pregunta del usuario). Hoy solo Windows Update `Type='Software'` (drivers excluidos) y gestores Linux; el antivirus solo entra si Defender publica firmas por Windows Update, sin distinguirlo. Propuestas: (a) módulo `security` con estado de Defender (`Get-MpComputerStatus`: protección en tiempo real, edad de firmas), (b) drivers pendientes de Windows Update (opt-in), (c) Linux: kernel en uso vs instalado, `needrestart`. *Hecho cuando:* el usuario elige y hay diseño en `decisiones/`.
- [ ] **I3 Disco físico en la lista de Hosts** (columna o tooltip). 
- [ ] **I4 SNMP**: contadores de interfaz, CPU y almacenamiento; gráfica.
- [x] **I5 Gráfica del historial de parches** (2026-09-26; sin revisión visual) y filtro booleano/`≤` en `FilterBar`.

## P4 — Aplazado por decisión del usuario
(2026-09-26: el buffer offline persistente del agente se integró; queda aplazado solo SMTP, dominio/VPS público y firma de binarios.)
SMTP · dominio/VPS público (D1 cubre LAN) · firma de binarios y auto-actualización · buffer offline persistente del agente.

## P1b — Carga de inventario y parches en local (ahora hay RAM; ya no espera a producción)
- [x] **L1 Ampliar `loadgen` y la siembra**: simular `POST /agent/inventory` (ports/services/processes: envío completo, latido con `rev` y `need_full`) y parches (`updates`) además de las métricas, con la cadencia real del agente (completo cada cambio, latido 24 h, parches cada 6 h ± 20 %). *Hecho cuando:* `run.sh load -inventory -updates` corre 1000 agentes con los tres tipos de envío.
- [x] **L2 Medir** ingest de inventario/parches (p50/p95, req/s, errores, 413/429), tamaño de la BD tras N horas simuladas, y el evaluador de alertas con 1000 hosts y las reglas de parches/puertos. *Hecho cuando:* tabla en `decisiones/2026-09-25-rendimiento.md`.
- [x] **L3 Carga sostenida (soak)**: 30–60 min con los 1000 agentes simulados; vigilar RAM/CPU del server, crecimiento de la BD, ciclo de mantenimiento/rollup y autovacuum. *Hecho cuando:* sin fugas ni degradación, o hallazgos corregidos.
- [x] **L4 `pg_prewarm`/`shared_buffers` y caché fría**: reproducir el primer acceso frío de una gráfica de 24 h (reiniciando Postgres) y decidir si hace falta `pg_prewarm`.

## P6 — Derivado de la carga (2026-09-26, `decisiones/2026-09-26-carga-inventario-parches.md`)
- [ ] **O1 Optimizar la vista `alert_candidates`** (420 ms por pasada con 1000 hosts; LATERAL de os_eol/parches por fila aunque la regla no exista). *Hecho cuando:* <100 ms con 1000 hosts y 8 reglas, sin cambiar resultados (test de equivalencia).
- [ ] **O2 Jitter del envío completo al arrancar el agente** (ráfaga de 3000 completos atrasa las métricas hasta 2,3 s).
- [ ] **O3 Log de solicitudes: muestrear los 429** (un flood deja una línea WARN por solicitud) y **variable de nivel de log** (`ARGOS_LOG_LEVEL`; hoy Info fijo).
- [ ] **O4 Repetir la soak** (30–60 min) en un equipo sin uso concurrente (degradación no reproducida).

## P5 — Meta: el server solo se instala y el agente solo se instala, se ejecuta y se enrola (pedido del usuario, 2026-09-26)
- [x] **T1 Config del agente (2026-09-26, `decisiones/2026-09-26-instalacion-un-paso.md`; sin probar en SO real) sin variables de entorno a mano.** Hoy inventario, parches, colector de pings (`ARGOS_PROBE_ALLOW`), `--privileged` y `ARGOS_ALLOW_INSECURE` se activan a mano en cada equipo (el instalador no pasa la config al servicio). Propuesta: `install` acepta flags (`--inventory ports,services,processes,updates`, `--probe-allow <cidr>`, `--privileged`, `--insecure`) que se guardan en la config persistente del servicio, y el modal "Agregar host" arma el comando con casillas. *Hecho cuando:* un host nuevo queda con inventario y parches funcionando con solo pegar el comando.
- [x] **T2 Server: instalación de un paso.** (2026-09-26; sin probar en Linux real) `install-server.sh` hoy exige Postgres 18 ya instalado y HTTPS aparte. Añadir `--install-postgres` (PGDG) y `--with-caddy` (TLS interno o dominio) para que un Linux limpio quede listo con un comando; crear el primer admin sin `argos-admin` a mano (contraseña generada o prompt). *Hecho cuando:* Ubuntu limpio → un comando → dashboard por HTTPS con admin.
- [ ] **T3 Validar instaladores del agente (a cargo del usuario en Linux y Windows; macOS sin equipo)** en Windows (servicio), Linux (systemd, ya visto en tu Ubuntu) y macOS (launchd, sin probar); `install.sh`/`install.ps1` servidos por el server; desinstalar limpio. *Hecho cuando:* checklist ejecutado por SO.
- [x] **T4 Actualización del agente** (2026-09-26: `argos-agent update` y `install.sh --update`; sin firmas, deuda aceptada) (hoy: `uninstall --keep-state` + `install`). Pedirá firmas (deuda aplazada). Mientras tanto, comando de actualización de una línea.
- [ ] **T5 D1 Primer despliegue real** en tu Linux con el paquete (ver `deploy/README.md`).
