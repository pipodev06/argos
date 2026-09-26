# Revisión de seguridad multi-cliente (servicio hospedado, 2026-09-26)

Contexto: el usuario es el único admin; sus clientes son `viewer` restringidos a grupos; agentes por HTTPS público (`https://argos.cloud-at-home.net.pe`) tras un túnel (Cloudflare Tunnel u otro), server en loopback. Revisión hecha leyendo código y tests (agente de seguridad); **no** se probó con un túnel real ni en un Linux real.

## Hallazgos
| # | Sev. | Dónde | Escenario | Estado |
|---|---|---|---|---|
| 1 | Alta (diseño) | `store/devices.go` (`CreateDevice`/`UpdateDevice`), `CollectorPings/SNMP` | El colector de un dispositivo puede ser de otro grupo/cliente: el agente del cliente A recibe IP y credenciales SNMP descifradas de un dispositivo del cliente B y escribe su estado. `SetHostGroup` también puede desfasarlos. | **Propuesto**: exigir mismo grupo (422) en create/update/`SetHostGroup`; rompe tests que usan hostA como colector de un dispositivo de B |
| 2 | Alta (diseño) | sin `DELETE /hosts`; FK sin CASCADE (00006, 00010) | Baja de cliente: `revoke` solo deshabilita; sus datos siguen (resumen horario 400 d, respaldos 14 d) y no hay borrado de usuarios | **Propuesto**: endpoint admin de purga por host/grupo (métricas por partición, hourly, chunks, discos, host) |
| 3 | Media | `alerts_handlers.go` `getAlert` | Un viewer restringido veía `channel_id/channel_name` de las entregas (canales son de admin y pueden nombrar a otro cliente) | **Corregido** con test |
| 4 | Media | `service_checks_handlers.go` | Un chequeo http con secreto y alcance `all` se entregaba (con su cabecera de auth y URL) a los agentes de todos los clientes | **Corregido**: 400 si hay secreto con alcance `all`; con test |
| 5 | Media | `all_groups DEFAULT true` (00019) | Un viewer nuevo ve todos los clientes hasta que el admin lo restringe (mitigado: `must_change_password`) | **Propuesto**: crear viewers con `all_groups=false` y pedir grupos al crear |
| 6 | Media | `store/users.go`, `api.go` (bloqueo por cuenta: 10 fallos, 15 min) | Con el server en Internet, quien conoce el usuario `admin` lo bloquea de forma sostenida (único admin) | **Decisión del usuario**: Cloudflare Access delante de `/api/v1/auth/*`, o bloqueo por (cuenta, IP) |
| 7 | Media | Historial/CSV (spec E) | Requisitos: ámbito con el mismo 404; `WriteTimeout` de 10 s corta el CSV (usar `SetWriteDeadline`); `reportSem` de 2 cupos monopolizable (cupo propio y 1 por usuario); nombre de archivo saneado; `csvSafe` para `mount`; decidir si el resumen horario (400 d) se limita a la retención efectiva | **Propuesto** (incorporar al implementarlo) |
| 8 | Baja/Media | ingest de métricas | Un token robado escribe hasta ~43 200 filas/día (15× lo normal) y agranda los chunks | **Propuesto**: descartar muestras a menos de ~10 s de la anterior del mismo host |
| 9 | Baja | token/cookie inválidos sin límite por IP; pool de 10 conexiones | Un flood con tokens basura llega a la BD | Regla de rate limit de Cloudflare sobre `/api/v1/*` |
| 10 | Baja | `ratelimit.go` (expulsa una clave al llegar a 100k) | Flood de IPs distintas puede vaciar cubetas | Aceptado tras el túnel |
| 11 | Media | binarios sin firma; `curl \| sh` | Quien controle server o túnel controla lo que los clientes instalan/actualizan (`agent update` es manual) | **Reprioriza la deuda de firma** (cosign + hash por canal aparte) |
| 12 | Baja | `deploy/argos-backup.sh` | El dump sin cifrar, en el mismo disco (0600), contiene a todos los clientes; un cliente dado de baja persiste 14 días | **Propuesto**: cifrar (age/gpg) y copiar fuera del host (elección de tecnología: pendiente del usuario) |
| 13 | Info | login | Devuelve `all_groups:false` (fail-closed, cosmético) | Sin tocar |

## Revisado sin hallazgo
Todas las rutas tienen acceso explícito y el test de enumeración exige clasificar cada GET; listados con `Scope` obligatorio y 404 idéntico fuera de ámbito; cursores firmados con huella que incluye ámbito y consulta; `/summary`, alertas, informes y CSV filtran por ámbito; enroll: solo admin, token ligado a grupo, un solo uso, 15 min, 401 uniforme, hostname fijado por el token; un token de agente robado solo escribe su host; sesiones (`__Host-`, Strict, CSRF, ID nuevo por login, Argon2id, `VerifyDummy`); `clientIP` con test de XFF falsificado; webhooks con bloqueo SSRF al conectar y sin redirects; retención por host solo admin, auditada y con `confirm_shrink`; decodificador de chunks acota la descompresión; systemd endurecido.

## Lista de verificación de despliegue tras el túnel
1. `ARGOS_PUBLIC_URL=https://argos.cloud-at-home.net.pe`, `ARGOS_TRUSTED_PROXIES=127.0.0.1/32`, `ARGOS_LISTEN_ADDR=127.0.0.1:8080` (sin proxies confiables todos los clientes comparten 127.0.0.1 y el límite de login/enroll se vuelve un DoS; conviene un aviso al arrancar).
2. El túnel conserva `Host`; ningún otro puerto expuesto; comprobar con `curl -H 'X-Forwarded-For: 1.2.3.4'` que la auditoría registra la IP real.
3. Postgres con dos roles, `ARGOS_AUTO_MIGRATE=false`, `scram-sha-256`, solo loopback; `argos.env` 0640; `ARGOS_SECRET_KEY` respaldada fuera del server.
4. Cloudflare: Access o al menos rate limit sobre `/api/v1/auth/login` y `/api/v1/*`; abiertos solo `/api/v1/agent/*`, `/install.*` y `/dl/*`; TLS ≥1.2 y "Always HTTPS".
5. Crear viewers y restringirlos a sus grupos de inmediato; revisar `GET /api/v1/auth/me`; chequeos con secreto solo con alcance de grupo o host; no poner colectores de un cliente sobre dispositivos de otro (#1).
6. Primer admin con `create-admin --must-change`; guardar la ruta de recuperación por CLI (`reset-password`) por el #6.
7. Respaldos cifrados y fuera del host (#12).
8. Antes de hospedar clientes: decidir el offboarding (#2) y la política del resumen horario (#7).

**No verificado:** comportamiento real con túnel (XFF/`Host` deducidos del código), instalador en Linux, agentes en Windows/macOS, canales laterales de tiempo, `maintenance_handlers`/`bulk_handlers` en detalle (solo por sus tests de ámbito), el efecto de los tests nuevos sin el arreglo (no se hizo la prueba inversa).
