# Inventario del host (puertos, servicios, procesos) — diseño aprobado (2026-09-25)

Aprobado por el usuario: el inventario va ANTES que SLA/ping-SNMP en prioridad de negocio (el SLA tanda 1 ya estaba en curso). Orden interno: 1 puertos en escucha, 2 servicios del SO, 3 procesos (top 100 agrupados por nombre); parches/vulnerabilidades del SO aparte, después. Decisión del orquestador: servicios en v1 solo Windows; en Linux, servicios v1.1 con `systemctl` de argv fijo (sin dependencia nueva, mismo patrón del chequeo `service`), no D-Bus. Diseño de security-engineer:

# Diseño: inventario del host (2026-09-25)

## Prioridad dentro del inventario
1 Puertos en escucha (proceso dueño + bind: expuesto vs solo local). 2 Servicios del SO (estado, tipo de inicio). 3 Procesos (agrupados por nombre, top N). Aparte, más adelante: parches/vulnerabilidades. Se confirma tu propuesta: los puertos dan el mayor valor de seguridad por byte y por CPU.

## Medición en esta máquina (Windows 11, agente SIN elevar, gopsutil v4.26.8 + x/sys)
- Conexiones inet: 0,5-1,6 ms (255 sockets, 50 en LISTEN). Dueño por PID: 25 de 50 listeners no se resolvieron (PID 0/4 y protegidos) -> "desconocido" real, no teórico.
- Procesos: listar 391 = 20-23 ms; `Name()` resolvió 390/390; `Exe()` falló en 198/391 (procesos de otros usuarios) -> NO se usa `Exe` (además revela rutas con nombres de usuario).
- Servicios: enumerar 340 nombres = 2,7 ms; detalle (config+estado) abriendo con `SERVICE_QUERY_CONFIG|QUERY_STATUS` = 335/340 en ~0,9 s en frío, sin elevar. Con `mgr.OpenService` (pide ALL_ACCESS) fallan todos: hay que abrir con derechos mínimos.
- Memoria: ~11 MB de heap del proceso de prueba; ~13 MB de asignación por ciclo de conexiones+procesos. Ciclo completo <1 s de CPU: 0,3 % de un núcleo si servicios va cada 5 min.
- No medido: Linux (mapeo socket->PID exige root o mismo uid salvo /proc sin hidepid: se espera "desconocido" para procesos ajenos) ni macOS.

## Campos seguros (lista blanca; todo lo demás se descarta)
- Puertos: `proto`(tcp/udp), `bind`(IP), `port`, `pid`(no se guarda en BD), `proc`(nombre base), `owner_known`(bool).
- Servicios: `name`, `display`, `state`(running/stopped/paused/other), `start`(auto/manual/disabled/other).
- Procesos: `name`, `count`, `rss_bytes`(suma) por nombre; top 100 por RSS.
- PROHIBIDOS: línea de comandos, variables de entorno, `Exe`, `Cwd`, `Username`, ruta binaria del servicio (lleva argumentos), cuenta de logon. Un test del agente escanea el paquete `inventory` y falla si aparece `Cmdline|Environ|Exe|Username|Cwd`. Nombres: UTF-8, sin control, ≤64 chars (display ≤128).
- Solo se reportan sockets LISTEN (TCP) y UDP sin conexión; los salientes/efímeros no se envían.

## Protocolo agente -> servidor
`POST /api/v1/agent/inventory` (token del agente, cuerpo ≤256 KiB, 413 si excede). Un envío por módulo: `{module, rev, items[], truncated, collected_at}`; `rev` = SHA-256 del listado canónico ordenado (procesos: solo nombres+conteos, sin RSS). Cadencia local: puertos 60 s, servicios 10 min, procesos 5 min. Envía solo si `rev` cambió (mín. 30 s entre envíos del mismo módulo, con jitter); si no, latido `{module, rev}` cada 6 h (procesos cada 30 min) sin items. El servidor responde `204`, o `409 need_full` si el `rev` del latido no coincide con su foto (se reenvía completo). Módulos por config local `inventory.modules` (lista; DEFAULT VACÍO = apagado; el servidor no puede activarlos). Cada envío declara el módulo, así la UI distingue "desactivado" de "sin datos". Topes del agente: puertos 512, servicios 1024, procesos 100; si excede, corta y marca `truncated:true`. El servidor revalida todo (enums, IP, puerto 1-65535, largos, cantidades) y rechaza con 400 genérico; límite de tasa por host (429).

## Esquema BD (migración 00021)
- `inventory_current(host_id FK CASCADE, module, rev, items jsonb, item_count, truncated, collected_at, received_at, PK(host_id,module))`: última foto.
- `inventory_changes(id bigserial, host_id FK CASCADE, module in ('ports','services'), kind in ('added','removed','changed','overflow'), key text, before jsonb, after jsonb, at timestamptz)`, índices `(host_id, at DESC, id DESC)` y `(module, kind, at)`. Procesos NO tienen historial (ruido).
- Ingest en transacción con `SELECT ... FOR UPDATE` de la fila actual; diff calculado en el servidor (clave puertos: proto+bind+port; servicios: name). La primera foto es línea base: no genera cambios. Tope de 1000 filas de cambio por host y día; el exceso se pliega en una fila `overflow`.
- Volumen: foto típica ≈ 5-30 KB/host (peor caso ≈ 90 KB) => 100 hosts ≈ 3 MB; 1000 hosts ≈ 30-40 MB (peor 90 MB). Cambios ~5/host/día x 90 días x ~300 B => 100 hosts 13 MB; 1000 hosts ≈ 135 MB (tope duro: 1000/día/host => 27 GB/1000 hosts en el peor caso teórico, imposible con el tope de tasa de ingest). Retención 90 días, purga en el job de retención existente.

## Endpoints GET (todos con `requireSession` + `requireInventory`; clase nueva `inventory` en `permissions_enum_test.go`)
- `/hosts/{id}/inventory` resumen: módulos con datos, `collected_at`, conteos, `truncated`.
- `/hosts/{id}/inventory/ports`, `/services`, `/processes`: lista completa acotada (≤512/1024/100), orden y filtro en cliente.
- `/hosts/{id}/inventory/changes?module=&kind=&cursor=&limit`: paginación por cursor con huella de `Scope.Key()` + host.
- Respuestas con `Cache-Control: no-store`. Sin permiso, host ajeno, inexistente o id inválido: el mismo 404 (ya lo hace el guard). `inventory` no se añade a `/hosts`, `/hosts/{id}`, summary ni eventos.
- El test de enumeración exige, para la clase `inventory`: 404 byte a byte idéntico sin `can_inventory`, con host ajeno y con host inexistente; y 200 solo con permiso + ámbito.

## Auditoría de lecturas
Evento `inventory_read` (actor, host_id, módulo; nunca contenido), deduplicado por (usuario, host, módulo) cada 5 min para no inundar con refetch. Denegación por falta de `can_inventory` (no por ámbito, para no crear un oráculo ni ruido) -> `authz_denied`, con límite de tasa. Ingesta: sin auditoría por envío; rechazos por validación con log warn con límite de tasa (sin cuerpo).

## Alerta futura "puerto nuevo expuesto" (solo diseño)
Se evalúa en el ingest sobre el diff: `kind=added`, módulo puertos, bind no-loopback (0.0.0.0, ::, IP de interfaz). Regla con ámbito all/grupo y lista de "puertos esperados" por grupo; clave de dedup host+proto+port; se auto-resuelve cuando el puerto desaparece; suprimida en la línea base y en mantenimiento; severidad warn. Reutiliza el motor de alertas y canales existentes.

## UI
Pestaña "Inventario" en `/hosts/:id`, visible solo si `me.role==='admin' || me.can_inventory` (ya en `/auth/me`); un 404 del servidor se trata como "sin acceso" sin revelar nada. Sub-secciones como chips de filtro (estado en URL): Puertos, Servicios, Procesos, Cambios. Tablas con el componente compartido (cabecera 32 px, filas 36 px, centradas, `aria-sort`), fuente normal (sin monoespaciada, ni IP/puertos). Puertos: badge "Expuesto" (icono de alerta + texto) / "Solo local" (muted); dueño "Desconocido" en muted. Servicios: badge de estado con icono + texto, filtro por estado/inicio. Cambios: `Pagination` por cursor. Cabecera "Actualizado hace X" y aviso "Lista truncada". Vacíos: "Módulo desactivado en este agente" vs "Sin datos aún".

## Tandas
1. Agente: paquete `inventory` (3 colectores, hash canónico, topes, lista blanca, config apagada por defecto, degradación), envío y planificación. Tests: hash estable ante orden/RSS, topes/truncado, `owner_known=false`, test de campos prohibidos, config vacía = cero llamadas, prueba real en Windows.
2. Servidor ingest: migración 00021, POST con validación, diff, línea base, tope de cambios, `need_full`, retención. Tests: tabla de validaciones, 413/429/401, diff, idempotencia por `rev`, carga 1000 hosts.
3. Servidor lectura: GET, cursor, auditoría, clase `inventory` en el test de enumeración. Tests con `argos_test`: permisos x ámbito x 404 idéntico, auditoría dedup, no-store.
4. UI: pestaña, tablas, filtros, estados vacíos. Vitest (visibilidad por permiso, desconocido, truncado) y revisión en navegador.

## Preguntas abiertas (decididas, no bloqueantes)
1. Default de módulos: apagado (mínimo privilegio); instalador con flag opcional. Decidido.
2. Servicios en Linux (systemd sin ejecutar `systemctl`, vía D-Bus): v1 solo Windows para servicios; puertos y procesos multiplataforma. Recomiendo confirmarlo.
3. Retención del historial 90 días: decidido; configurable por env si lo pides.

## Tanda 1 hecha (2026-09-25): agente (`agent/internal/inventory/`)
- Módulos con `ARGOS_INVENTORY_MODULES=ports,services,processes` (vacío = nada corre). `rev` = SHA-256 del listado ordenado (puertos sin pid; procesos solo nombre y conteo). Envío solo si cambia el `rev`, mínimo 30 s entre envíos completos; latido sin items cada 6 h (30 min en procesos). `409` solo cuenta como `need_full` en latidos (a un envío completo es fallo). `404/405` desactiva el módulo con backoff 1–24 h y un solo aviso. Topes: 512 puertos, 1024 servicios, 100 procesos, cuerpo ≤256 KiB (`truncated`). Servicios solo Windows (`QUERY_CONFIG|QUERY_STATUS`); test que escanea el código fuente y falla si aparecen campos prohibidos.
- Deuda: no se lee `Retry-After` en 429; puertos/procesos en Linux y macOS solo compilan (sin probar); servicios Linux v1.1; `-race` sin gcc.
- Intermitencia real hallada al verificar: `TestServiceModeResumesAfterReEnroll` falló 2 de 3 veces en la corrida completa de `check-go.sh agent` con `rename ...agent.json: Acceso denegado` (Windows, reemplazo atómico de `agent.json` mientras algo —antivirus/indexador— retiene el archivo) y pasa siempre en aislamiento. Es una debilidad de producción, no solo del test: la escritura atómica de credenciales debería reintentar brevemente ante `ERROR_ACCESS_DENIED`/`SHARING_VIOLATION`. Pendiente (deuda media).

## Tanda 2 hecha (2026-09-25): ingest en el servidor
- Migración 00021, `POST /api/v1/agent/inventory` (lista blanca estricta: 400 ante cmdline/exe/username/environ/`host_id`; el pid llega pero no se guarda; con `owner_known=false` el servidor fuerza `proc` a vacío), `409 need_full`, límite 20/min por host (429), diff contra línea base con tope de 1000 cambios por host y día (fila `overflow`), idempotente ante reenvío del mismo `rev`. Retención `ARGOS_INVENTORY_RETENTION_DAYS` (90, mín 30) en el mantenimiento existente.
- Decisiones: elementos repetidos se descartan (SO_REUSEPORT da puertos repetidos legítimos; nombres de servicio pueden chocar al recortarse) en vez de 400.
- Deuda: sin log warn con límite de tasa en rechazos de validación (el diseño lo pedía); los dos módulos con historial comparten el tope diario sin serializarse (dos envíos simultáneos pueden pasarse por unas filas); 401 sin token no probado por separado.
- Pendiente: tanda 3 (GET + auditoría `inventory_read`) y tanda 4 (UI). Por ahora nada lee lo guardado.
