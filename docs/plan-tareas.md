# Plan de tareas (solo lo abierto; actualizado 2026-10-17)
Una tarea = un commit (o pocos). Al cerrar una, bórrala de aquí y pasa su línea a `decisiones/archivo.md`. Orden = prioridad. Cada tarea trae su "hecho cuando".

## Ahora
- [ ] **T3 Validar instaladores del agente** (a cargo del usuario; macOS sin equipo): Windows (servicio), Linux (systemd, ya visto en Ubuntu), macOS (launchd); `install.sh`/`install.ps1` servidos por el server; desinstalar limpio. *Hecho cuando:* checklist ejecutado por SO.

## Después
- [ ] **B19 El plan también limita el resumen horario, eventos, alertas e inventario** (decisión del usuario pendiente). (Media, M)
- [ ] **B25 Revisión visual en navegador** de: parches, historial por fecha, configuración/retención (con Perfiles SNMP), purga, modal "Agregar host".
- [ ] **I1 Windows con servicio privilegiado**: `root_home` conoce el dueño de 41/103 puertos sin privilegios vs 23/23 en Ubuntu. Instalar con `--privileged` y comparar. *Hecho cuando:* medido y documentado.
- [~] **O4 Repetir la soak** (30–60 min) en un equipo libre (2026-09-27: en máquina compartida, 0 errores, sin fuga).

## Aplazado (decisión del usuario; no escribir código sin pedido)
- Aplazado: producción completa (D1 primer despliegue, H4 bloqueo del admin desde Internet, H6 respaldos cifrados fuera del host; `deploy/` y `scripts/package-server.sh` se quitaron el 2026-10-17 y siguen en git, commit `a75145e`) · SMTP · dominio/VPS público · restauración por cliente y DR · MFA, sesiones avanzadas y recuperación de contraseña · informes programados/PDF · i18n y accesibilidad móvil · consumo/facturación por cliente más allá de las cuotas.
