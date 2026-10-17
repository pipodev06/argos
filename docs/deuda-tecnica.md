# Deuda técnica (repaso completo el 2026-09-26, ajustado el 2026-10-17; lo resuelto está en `decisiones/archivo.md`)

Las TAREAS con "hecho cuando" viven en `plan-tareas.md`; aquí solo los límites y riesgos conocidos. Hallazgos de seguridad del servicio hospedado: `decisiones/2026-09-26-seguridad-multicliente.md`.

## Resumen por severidad (leer solo esto; abrir la sección enlazada si tocas esa área)
### Alta (antes de entrar el primer cliente)
- Bloqueo de la cuenta admin desde Internet (único admin), baja/offboarding con respaldos sin limpiar, respaldos sin cifrar -> [Servicio hospedado](#servicio-hospedado)
- Instalación real (agente Windows/Linux/macOS y `install-server.sh` en Linux) sin ejecutar en un SO real -> [Instalación](#instalación-de-un-paso-sin-probar-en-so-real)
### Media
- Pantallas nuevas sin revisar en navegador (Resumen sí lo revisó el usuario): parches (gráfica y filtros), modal "Agregar host", historial por fecha, configuración/retención, purga, dispositivos (deshabilitar) -> [Frontend](#frontend)
- Estado en memoria mono-instancia (rate limits, clave HMAC de cursores, dedup de auditoría) -> [Seguridad](#seguridad)
- Auditoría best-effort (no transaccional) -> [Seguridad](#seguridad)
- Monitorear el propio servicio: un `DETACH`/empaquetado que falla se ve en "Estado del servicio" y en el aviso del Resumen (admin), pero no llega por webhook; fuera del dashboard, vigilar `GET /metrics` (`argos_task_last_ok_age_seconds`) o `/health` con un monitor externo -> [Operación](#operación)
### Baja
- Límites de colectores (batería, discos, `host_info`, red virtual), Linux solo systemd, macOS sin rotación de log, Windows con estado de solo lectura -> [Agente](#agente-y-servicio)
- Series: primer tramo sin tasa, datos tardíos >3 h no se reprocesan, partición ausente tras una parada larga -> [Datos](#datos-y-api)
- Sin test de `main`/SIGTERM real; tests de BD se SALTAN en silencio sin `ARGOS_TEST_DATABASE_URL` -> [Operación](#operación)

## Servicio hospedado
- Ver la tabla de hallazgos y la lista de verificación de despliegue tras el túnel en `decisiones/2026-09-26-seguridad-multicliente.md`. Abiertos: #5 (viewers con `all_groups=true` por defecto), #6 (bloqueo de cuenta; decisión del usuario), #11 (firma de binarios), #12 (respaldos cifrados; decisión del usuario) y la política del resumen horario (400 d aunque la retención del cliente sea menor; decisión del usuario).
- La purga (H2) no limpia respaldos (14 días) ni `audit_log`. El modelo de canales de notificación es global (solo admin): un cliente no puede tener sus propios canales; SMTP aplazado. Los topes de recursos (hosts, dispositivos, chequeos, reglas) son globales, no por cliente.

## Instalación de un paso (sin probar en SO real)
- Agente: `install`/`update` e instaladores (`install.sh`/`install.ps1`) sin ejecutar en Windows/Linux/macOS reales (macOS sin equipo); `update` verifica solo el sha256 del manifiesto (sin firmas independientes); cambiar `--privileged`/`--ca-file` de un servicio ya instalado exige `uninstall --keep-state` + `install`; `--token` visible en `ps` durante la instalación; el manifiesto se verifica solo al arrancar el server.
- Server: `install-server.sh` (apt/PGDG/Caddy/ufw/systemd) sin ejecutar en Linux; la URL de la BD viaja un instante por argv a `psql`; sin verificar huellas de las claves GPG; si el usuario elegido ya existe sin ser admin, `create-admin` falla al final.
- Falta `systemd-analyze security` de ambas unidades, la lectura de `/sys/class/power_supply` con la unit endurecida y `dscl`/`launchctl` reales. Si `PrivateDevices=true` o `MemoryDenyWriteExecute=true` rompen una distro, quitar solo esa línea y anotarlo.

## Seguridad
- Server fuera de loopback solo tras HTTPS (túnel/proxy o `ARGOS_TLS_CERT/KEY`) con `ARGOS_PUBLIC_URL` y `ARGOS_TRUSTED_PROXIES`; sin proxies confiables todos los clientes comparten la IP del proxy (el límite de login/enroll se vuelve un DoS). Verificado solo TLS directo con cert autofirmado y curl; **no con un túnel real**.
- Roles de BD: en el modo de un solo rol (dev, o `--db-url` sin `--migrate-db-url`) el server es dueño y puede saltar el trigger de `audit_log`; el rol de runtime es dueño de las tablas particionadas y tiene CREATE en `public` (límite aceptado; ver `2026-09-25-roles-bd.md`).
- Rate limits, fallos de agente, dedup de auditoría y la clave HMAC de cursores viven en memoria (se pierden al reiniciar; no se comparten entre réplicas → `ARGOS_CURSOR_KEY`/BD si se escala).
- Permisos por grupo: `check_name` de una regla de alcance `all` puede nombrar un chequeo de un grupo ajeno (fuga menor aceptada); los cambios de permisos de un usuario se ven al recargar (el front lee `/auth/me` al cargar).
- Sin tests del prompt sin eco de `create-admin`/`reset-password` (solo stdin; los flujos de store/handlers sí). Ingest: el tope H5 (4 s por `ts`) vive en memoria y se pierde al reiniciar. Firma de binarios (H7): sin rotación de la clave de firma; si se pierde/filtra, todo agente existente queda sin poder `update` hasta reinstalarse.

## Agente y servicio
- Buffer offline persistente: los extras (discos/`host_info`) de un reporte descartado por tope no se persisten; tras reiniciar con buffer viejo reenvía a 1 reporte cada 2,5 s.
- Windows: el servicio solo LEE el estado (ACL de solo lectura; con rotación de token habrá que darle escritura); `install` deja binario y usuario si falla un paso posterior (solo en Windows se revierte el servicio). macOS: sin rotación de `/var/log/argos-agent.log`. Linux exige systemd.
- Batería: `Unknown` de sysfs omite el bloque; solo `BAT*`; baterías múltiples sin ponderar. Discos: excluidos red y `fuse.*`, Windows solo unidades fijas, sin protección contra `statfs` colgado. `host_info`: IPv6 temporales rotan; interfaces virtuales filtradas por prefijo; `os_name` en Linux = id de distro; los contadores de red suman interfaces virtuales.
- Apagado por señal solo probado por cancelación de contexto (no Ctrl+C/SIGTERM real en Windows).

## Datos y API
- Con `ARGOS_METRICS_MAINTENANCE_INTERVAL` muy bajo (solo pruebas) el check `maintenance` de B6 puede dar falso crit durante un backfill grande (el umbral se calcula sobre el intervalo configurado); con el valor de produccion (5 min) no pasa.
- Sin benchmark de tiempo automatizado para `alert_candidates` (solo medición manual con `EXPLAIN`); un gate de rendimiento en CI necesitaría decidir margen y máquina de referencia. Entregas `dead` de antes de la migración 00035 llevan `dead_at = created_at` (backfill sin fecha real).
- **`metrics_chunks`** (`decisiones/2026-09-26-detalle-comprimido-retencion.md`; probado con volumen real el 2026-09-27, ver `decisiones/2026-09-27-e3-volumen-disk-chunks.md`; `disk_samples` ya tiene su propio chunk, `disk_chunks`, con retención solo global): un chunk que falle siempre deja la partición sin borrar y loguea ERROR cada ciclo; validaciones aceptan 730 d aunque `HOT_DAYS` sea mayor que 8; la lista de hosts no muestra la retención propia.
- Si el server/mantenimiento estuvo parado varios días no se crean particiones pasadas: una muestra con `ts` en ese hueco da 503 hasta caer fuera de la ventana de 7 días del ingest (sin backfill).
- Datos tardíos de más de 3 h no se reprocesan en `metrics_hourly`; el promedio de buckets de 3 h/12 h pondera tasas ya promediadas (error <2 % en test). `MetricSeries*` no lee la muestra previa al rango (el primer tramo no tiene tasa). El CSV crudo del historial usa hueco fijo de 10 min y el JSON la mediana. `disks` del detalle mira 7 días. `summary.by_group` rotula "Sin grupo" en español.
- Un `DETACH ... CONCURRENTLY` espera a todas las transacciones anteriores (una consulta larga aplaza el borrado; con `lock_timeout` 5 s falla con ERROR y reintenta cada ciclo, sin alerta propia). Con varias instancias cada una tiene su propia gracia de arranque del detector (el UPDATE es idempotente).
- `host_events`: el pasado no se reconstruye; una caída se subcuenta hasta `down_after_seconds`; editar una ventana de mantenimiento cambia el % de disponibilidad pasado.

## Frontend
- Bundle dividido (entrada 48 kB; Recharts en su chunk). Vitest (361 tests) con tests de componentes y axe (contraste no medible en jsdom); sin probar en navegador ni los rangos 30d/90d/1y contra la API real. En la tabla de dispositivos el aviso de desfase puede desbordar los 36 px de fila en pantallas angostas. El origen para `--insecure` del modal es `window.location.origin`.

## Operación
- El server sirve el frontend con `ARGOS_WEB_DIR` (fallback SPA, CSP con `style-src 'unsafe-inline'`); `ARGOS_LOG_LEVEL` disponible y los 429 muestreados.
- Tests de BD: sin `ARGOS_TEST_DATABASE_URL` (BD que termine en `_test`) se SALTAN en silencio y el resultado sigue en verde; verificar siempre con la variable (el CI define la suya). `check-go.sh --race` del server dio un FALLO suelto una vez; no se reprodujo en el intento mas reciente (2026-09-27, gcc instalado, 269s en verde). Si vuelve a pasar, guardar el log completo antes de reintentar. La suite tarda ~90 s cuando la máquina está libre; con carga (Firefox/antivirus) llegó a ~200 s.
- Sin test de `main` (cableado/apagado por señal); e2e cerrados con `taskkill /F` en Windows.
- Diseños aprobados y ya implementados: ping/SNMP, inventario, parches, SLA/CSV, permisos por grupo (ver el índice). Canal SMTP: aplazado por el usuario.
