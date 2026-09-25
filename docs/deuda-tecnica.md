# Deuda tecnica (actualizada 2026-09-25; lo resuelto esta en `decisiones/archivo.md`)

Resuelto el 2026-09-25 (ya no es deuda): CI en GitHub Actions (web + go server/agent con `-race` en Linux, verde); SPA servida por el server con fallback y CSP (`ARGOS_WEB_DIR`); la CSP con `style-src 'unsafe-inline'` resuelve el `<style>` de `react-remove-scroll`; runner de tests del frontend (Vitest); BD real `argos` migrada hasta 00019 (se aplica al arrancar el server); etiquetas eliminadas; canal Discord verificado por el usuario (con sufijo `/slack`).

## Resumen por severidad (leer solo esto; abrir la seccion enlazada si tocas ese area)
### Alta
- Exponer el server fuera de loopback solo tras HTTPS + `ARGOS_PUBLIC_URL`; no verificado con proxy real -> [Seguridad](#seguridad)
- Un solo rol de BD (migra y opera): el trigger append-only de `audit_log` se puede saltar -> [Seguridad](#seguridad)
- Instalacion real del servicio del agente sin probar en Windows/Linux/macOS -> [Agente](#agente-y-servicio)
### Media
- Pantallas nuevas sin revisar en navegador (permisos/Acceso, chequeos, mantenimiento, alertas): esperan observaciones del usuario -> [Frontend](#frontend)
- Buffer offline solo en memoria (120): un reinicio con el server caido pierde datos -> [Agente](#agente-y-servicio)
- Agente Windows: el reemplazo atómico de `agent.json` puede fallar con "Acceso denegado" si el antivirus o el indexador retienen el archivo (falla intermitente de `TestServiceModeResumesAfterReEnroll`); reintentar brevemente -> [Agente](#agente-y-servicio)
- Sin firma de binarios ni auto-actualizacion; `install` no pasa config de entorno al servicio -> [Agente](#agente-y-servicio)
- Estado en memoria mono-instancia (rate limits, clave HMAC de cursores) -> [Seguridad](#seguridad)
- Auditoria best-effort (no transaccional) -> [Seguridad](#seguridad)
- Tests de frontend parciales (Vitest, 55 tests: sin `ruleForm`, `checkForm`, `chart`, `alertQuery` ni componentes) y sin probar contra la API real (30d/90d/1y) -> [Frontend](#frontend)
- Lista/resumen sin medir con 1000 hosts; serie de 7d ~115 ms/host; filtro de ambito de alertas sin EXPLAIN con volumen (el indice `alerts_host_idx` ya existe desde 00015, no hace falta otro) -> [Datos](#datos-y-api)
- `host_events` ya existe (00014; extendido en 00020, retencion 400 d); faltan API y pantalla de SLA (punto 7, tandas 2 y 3); `DETACH` aplazado no alerta -> [Datos](#datos-y-api)
- Webhook: Discord solo funciona con el sufijo `/slack` en la URL (el payload lleva `text`, no `content`); sin formato nativo de Discord -> [Operacion](#operacion)
### Baja
- Bloqueo por cuenta permite bloquear a un usuario ajeno; sin tests del prompt de `create-admin` -> [Seguridad](#seguridad)
- `govulncheck`: GO-2026-5932 en `x/crypto` (sin fix; no se llama) -> [Seguridad](#seguridad)
- Bateria, discos, `host_info`, red virtual: limites de los colectores -> [Agente](#agente-y-servicio)
- Linux solo systemd; macOS sin rotacion de log; Windows estado solo lectura; `install` deja restos si falla -> [Agente](#agente-y-servicio)
- Series: primer tramo sin tasa, datos tardios >3 h, tasas ponderadas aproximadas, particion ausente tras parada larga -> [Datos](#datos-y-api)
- Sin request logging ni `/health` con chequeo de DB; sin test de `main`/SIGTERM real -> [Operacion](#operacion)
- `-race` no corre en local (falta gcc en Windows); lo cubre el CI de Linux -> [Operacion](#operacion)
- Permisos por grupo: `check_name` de una regla de alcance `all` puede nombrar un chequeo de un grupo ajeno (fuga menor aceptada); los cambios de permisos de un usuario se ven en su navegador al recargar (el frontend lee `/auth/me` al cargar) -> [Seguridad](#seguridad)

## Seguridad
- Server fuera de loopback (`ARGOS_LISTEN_ADDR` no loopback) solo tras HTTPS (proxy inverso o `ARGOS_TLS_CERT/KEY`) y con `ARGOS_PUBLIC_URL`; verificado solo TLS directo con cert autofirmado y curl.
- Rol de BD unico: separar migrador/runtime (recomendado, no decidido; ver indice).
- Rate limits y contadores de fallos de agente en memoria (pierden estado al reiniciar, no se comparten entre replicas). La clave HMAC de los cursores de `GET /hosts` es aleatoria por proceso (reinicio o varias instancias -> 400 `invalid_cursor`); si se escala, pasar a BD/Redis y mover la clave a `ARGOS_CURSOR_KEY`.
- Auditoria best-effort: si la escritura falla solo se loguea; revisar si se exige no repudio.
- El bloqueo por cuenta (15 min por 10 fallos) permite bloquear a proposito a otro usuario; mitigado por limite por IP y `reset-password`.
- Sin tests del prompt sin eco de `create-admin`/`reset-password` (solo stdin por tuberia; los flujos de store/handlers si).
- `govulncheck` reporta GO-2026-5932 en `golang.org/x/crypto@v0.57.0` (sin version corregida; el codigo no llama la funcion afectada); preexistente.

## Agente y servicio
- Instalacion real sin probar en ningun SO (requiere elevacion; hay unit tests, ACL real en dir temporal, cross-compile). Falta: `install` -> `enroll` -> `start` en cada SO, `systemd-analyze security argos-agent`, lectura de `/sys/class/power_supply` con la unit endurecida, `dscl`/`launchctl` reales, log en el registro de eventos. Si `PrivateDevices=true` o `MemoryDenyWriteExecute=true` rompen una distro, quitar solo esa linea y anotarlo.
- Buffer offline solo en memoria (120 reportes, descarta el mas viejo); persistir exige elegir libreria (no aprobada).
- Sin firma de binarios (SmartScreen, Gatekeeper/notarizacion; un binario Go en darwin/arm64 con firma ad-hoc del linker corre pero no esta notarizado) ni auto-actualizacion (actualizar = `uninstall --keep-state` + `install`; auto-update solo con firmas).
- Config del servicio por env (intervalo, `ARGOS_ALLOW_INSECURE`...) no la pasa `install`: ponerla a mano (drop-in de systemd, `EnvironmentVariables` del plist, `Environment` del registro). Un server `http` remoto no funciona como servicio sin `ARGOS_ALLOW_INSECURE`.
- Windows: el servicio solo LEE el estado (ACL de solo lectura); con rotacion de token habra que darle escritura en `%ProgramData%\argos-agent` y revisar el modelo de amenazas. `install` deja binario y usuario aunque falle un paso posterior (solo en Windows se revierte el servicio); `uninstall` los limpia.
- macOS: sin rotacion de `/var/log/argos-agent.log` (newsyslog no reabre el archivo que mantiene launchd); con el server caido se escribe cada ciclo. Linux exige systemd (sin sysvinit/OpenRC/Alpine).
- Bateria: estado `Unknown` de sysfs omite el bloque (se podria mirar `AC*/online`); solo `BAT*` (no `macsmc-battery`, `qcom-battmgr-bat`); baterias multiples sin ponderar por capacidad.
- Discos: excluidos red y `fuse.*` (p. ej. `mergerfs`); Windows solo unidades fijas (un USB marcado removible no se reporta); sin proteccion contra `statfs` colgado en un montaje local roto.
- `host_info`: IPv6 temporales rotan (envios extra, tope 1 cada 5 min); interfaces virtuales filtradas por prefijo de nombre (puede dejar pasar alguna); `os_name` en Linux = id de distro ("ubuntu"). Contadores de red suman interfaces virtuales (docker, veth, VPN).
- Apagado por senal solo probado por cancelacion de contexto (no Ctrl+C/SIGTERM real en Windows).

## Datos y API
- Migracion 00006: `ADD CONSTRAINT ... CHECK` y resumen inicial del historial legacy (48 h por ciclo) no medidos con decenas de millones de filas; con `argos` (~1000 filas) es instantaneo (la BD real ya esta migrada hasta 00019).
- Lista de hosts y resumen sin medir con 1000 hosts (orden por cpu/mem/disco sobre `host_latest` con seq scan; revisar planes). Serie de 7d ~115 ms por host con 60k muestras (calculo en CPU por peticion); si crece: rollup por hora / TimescaleDB. Sin test de carga automatico de la consulta.
- `host_events`: diseño aprobado en `decisiones/2026-09-25-sla-csv.md` (trigger sobre `hosts`, retencion 400 d, siembra inicial; el pasado no se reconstruye; una caida se subcuenta hasta `down_after_seconds`; editar una ventana de mantenimiento cambia el % pasado). Un `DETACH ... CONCURRENTLY` espera a todas las transacciones anteriores (consulta larga o "idle in transaction" aplaza el borrado; con `lock_timeout` 5 s falla con log ERROR y reintenta cada ciclo, sin alerta propia: avisar cuando exista el motor de alertas).
- Si el server/mantenimiento estuvo parado varios dias no se crean particiones pasadas: una muestra con `ts` en ese hueco da 503 hasta caer fuera de la ventana de 7 dias del ingest (el buffer del agente se limpia solo; sin backfill).
- Datos tardios de mas de 3 h no se reprocesan en `metrics_hourly`. El promedio de buckets de 3 h/12 h es ponderado por `samples` de tasas ya promediadas (error <2 % en el test).
- `MetricSeries`/`MetricSeriesHourly` no leen la muestra previa al rango: el primer tramo no tiene tasa; con solo 2 muestras muy separadas no se marca hueco.
- `disks` del detalle mira 7 dias; un equipo apagado mas tiempo no muestra discos. `summary.by_group` rotula "Sin grupo" en espanol (el front puede usar `group_id: null`).
- `IngestMetrics` no distingue 404/409 si el host se borra entre auth e ingest (FK -> 500); improbable (no hay borrado de hosts). Con varias instancias cada una tiene su propia gracia de arranque del detector (sin coordinacion; el UPDATE es idempotente).

## Frontend
- Vitest (`npm test`, 55 tests junto al codigo; corre en `check-web.sh` y CI): sin cobertura de `ruleForm`, `checkForm`, `chart`, `alertQuery`, otros hooks ni componentes. Sin probar en navegador (visual, teclado), ni los rangos 30d/90d/1y ni el contrato de grupos/summary contra la API real. Bundle ~660 kB (Recharts). "Ver procesos y servicios" es un Checkbox (no hay componente Switch).

## Operacion
- El server sirve el frontend con `ARGOS_WEB_DIR` (fallback SPA, CSP con `style-src 'unsafe-inline'`, ruta invalida = no arranca; un POST a ruta desconocida da 405). Lo que sigue pendiente es HTTPS/proxy para exponerlo fuera de loopback (ver Seguridad).
- Tests de BD: sin `ARGOS_TEST_DATABASE_URL` (BD que termine en `_test`; local: `postgres://argos:argos@localhost:5432/argos_test?sslmode=disable`) se SALTAN en silencio, y el resultado sigue en verde; verificar siempre con la variable definida. El CI define la suya.
- `-race`: sin gcc en Windows local; corre en el CI de Linux (verde el 2026-09-25).
- Sin middleware de request logging ni `/health` con chequeo de DB. Sin test de `main` (cableado/apagado por senal); e2e cerrados con `taskkill /F` en Windows.

## Fase 3 instaladores (2026-09-24)
- ALTA: binarios servidos sin firma: un server comprometido da codigo como root/SYSTEM en hosts nuevos. Requiere firmas independientes del server antes de cualquier auto-update. Ver decisiones/2026-09-24-fases-2-y-3.md.
- Media: manifiesto verificado solo al arrancar; `--token` visible en `ps` durante la instalacion; instaladores y PowerShell no ejecutados de verdad (solo `bash -n` y parseo); bundle web >500 kB.

## Diseños aprobados, sin implementar (2026-09-25)
- Ping y SNMP para equipos sin agente (punto 8): `decisiones/2026-09-25-ping-snmp.md` (agente colector con `ARGOS_PROBE_ALLOW`, `hosts.kind='device'`, SNMP v3 por defecto, `gosnmp` aprobado, solo interfaces marcadas). Se implementa despues del SLA.
- Inventario de procesos/servicios/puertos (permiso `can_inventory` y guard `requireInventory` ya listos; falta el agente y el endpoint) y estado de parches/vulnerabilidades del SO: siguientes tras 7 y 8.
- Canal de correo (SMTP): no por ahora (decision del usuario).
