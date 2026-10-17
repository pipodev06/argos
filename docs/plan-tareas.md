# Plan de tareas (solo lo abierto; actualizado 2026-10-17)
Una tarea = un commit (o pocos). Al cerrar una, bórrala de aquí y pasa su línea a `decisiones/archivo.md`. Orden = prioridad. Cada tarea trae su "hecho cuando".

## Bloquea entrar al primer cliente
- [ ] **D1 Primer despliegue real** (= B4/T5) con `deploy/` (ver `deploy/README.md`): instalar, `systemctl status`, `systemd-analyze security argos-server`, respaldo (`systemctl start argos-backup.service`) y restaurar el dump en una BD de prueba. *Hecho cuando:* login por HTTPS desde otro PC y un agente enrolado contra ese server; ajustes anotados.
- [ ] **T3 Validar instaladores del agente** (a cargo del usuario; macOS sin equipo): Windows (servicio), Linux (systemd, ya visto en Ubuntu), macOS (launchd); `install.sh`/`install.ps1` servidos por el server; desinstalar limpio. *Hecho cuando:* checklist ejecutado por SO.
- [ ] **H4 Bloqueo de la cuenta admin desde Internet** (= B3, #6, decisión del usuario): Cloudflare Access delante de `/api/v1/auth/*` o bloqueo por (cuenta, IP); aviso al arrancar sin proxies confiables.
- [ ] **H6 Respaldos cifrados y fuera del host** (= B5, #12; elección del usuario: age/gpg + destino).

## Después
- [ ] **B19 El plan también limita el resumen horario, eventos, alertas e inventario** (decisión del usuario pendiente). (Media, M)
- [ ] **B25 Revisión visual en navegador** de: parches, historial por fecha, configuración/retención (con Perfiles SNMP), purga, modal "Agregar host".
- [ ] **I1 Windows con servicio privilegiado**: `root_home` conoce el dueño de 41/103 puertos sin privilegios vs 23/23 en Ubuntu. Instalar con `--privileged` y comparar. *Hecho cuando:* medido y documentado.
- [~] **O4 Repetir la soak** (30–60 min) en un equipo libre (2026-09-27: en máquina compartida, 0 errores, sin fuga).

## Aplazado (decisión del usuario; no escribir código sin pedido)
- Aplazado: SMTP · dominio/VPS público (D1 cubre LAN) · restauración por cliente y DR · MFA, sesiones avanzadas y recuperación de contraseña · informes programados/PDF · i18n y accesibilidad móvil · consumo/facturación por cliente más allá de las cuotas.
