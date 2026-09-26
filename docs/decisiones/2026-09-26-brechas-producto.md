# Brechas de producto por flujo (análisis de solo lectura, 2026-09-26)

Método: agente de análisis leyendo `Routes()`, handlers, store, pantallas web, agente y despliegue; **no ejecutó nada**. Los "no lo encontré" se basan en `grep` y en la lista de rutas y podrían existir con otro nombre. Contexto: servicio hospedado (un admin, clientes = grupos + viewers restringidos). La lista priorizada con estado vive en `plan-tareas.md` (P9, B1–B30).

## Lo que hay por flujo (resumen)
1. **Alta de un cliente**: crear grupo, retención, usuarios, permisos, enrolar (token de 15 min por host, máx. 50), dispositivos, reglas y canales ✔; faltan: entidad "cliente" (grupo = cliente), viewers con grupos desde el alta (nacen con `all_groups=true`), retención en `POST /groups` (hay que crear y luego editar), listado/revocación de tokens pendientes, alta masiva, plantillas de reglas.
2. **Vida del cliente (viewer)**: ve hosts, resumen, alertas, chequeos, informes y su historial con ámbito ✔ (lo mejor cubierto); no puede reconocer alertas ni silenciar, no recibe notificaciones propias sin que el admin cree el webhook, sin correo (SMTP aplazado), sin recuperación autoservicio, sin listado de sesiones, sin MFA.
3. **Usuarios (admin)**: crear/editar/deshabilitar/restablecer ✔; **no hay `DELETE /users`**, invitaciones, ni rol delegado (solo `admin`/`viewer`).
4. **Vida de un host/agente**: renombrar, mover, deshabilitar, purgar ✔, `update` ✔; faltan: reactivar un host con agente, rotar token, re-enrolar conservando el historial (`enroll --force` crea un host nuevo y deja el anterior huérfano), detección de hostnames duplicados, estado "nunca reportó", vista de versiones del agente, aviso del server al desinstalar.
5. **Alertas**: reglas por grupo/host/dispositivo ✔, mantenimiento, resolución notificada, límites y agrupación ✔; faltan plantillas, silenciar una regla puntual, flapping, escalamiento; entregas `dead` solo visibles en el detalle de cada alerta (un canal roto pasa desapercibido).
6. **Informes**: disponibilidad/CSV, alertas CSV e historial ✔; solo ventanas móviles 7/30/90 d (tope 91 d aunque `host_events` guarda 400), sin PDF, programación ni marca; inventario y parches no se exportan.
7. **Suscripciones**: **no existe** (planes, límites por cliente, consumo, avisos). Los topes son globales.
8. **Operar el servicio**: `/health` solo mira la BD; sin `/metrics`; respaldos sin cifrar ni restore probado; sin runbook de DR; sin rotación de `ARGOS_SECRET_KEY`; actualizar el server nunca se ejecutó en Linux real.
9. **Baja de un cliente**: purga de host y grupo ✔ con UI; sin "exportar todo el cliente" previo, sin constancia, y quedan canales webhook y perfiles SNMP del cliente, `audit_log` y respaldos.
10. **Experiencia**: estados vacíos ✔; sin ayuda en la UI, sin i18n (español fijo), accesibilidad y móvil sin revisar; solo 1 test `.tsx` (Vitest cubre funciones puras).

## Riesgos de producto (no son bugs)
- **R1 Canales globales sin dueño**: `SetRuleChannels` solo comprueba que el canal exista; una regla de alcance `all` asignada al canal de un cliente enviaría a su webhook los hostnames de otros clientes. **No estaba en la revisión de seguridad; se agregó como B2.**
- R2 Topes globales (200 reglas, 50 canales, 50 tokens activos) se agotan con 20–50 clientes. R3 Único admin bloqueable. R4 Estado en memoria mono-instancia. R5 Retención que no depende del plan (resumen horario 400 d, alertas 365 d, eventos, disco, chequeos, inventario). R6 Confianza en binarios sin firma. R7 Alertas dependientes de un solo webhook. R8 Recuperación de contraseñas manual sin SMTP.

## Decisiones de producto pendientes
Reconocer alertas por viewers (`can_ack`); dueño de canales por grupo y validación regla-canal; qué retención aplica el plan al resumen horario; si el "cliente" pasa a ser una entidad con varios grupos; SMTP.
