# Auditoría y limpieza (2026-10-17)

Pedido del usuario: react-doctor, seguridad, UX, rendimiento, README y quitar lo sin terminar.

- **Seguridad**: Go 1.27.0 -> 1.27.2 en `server/go.mod` y `agent/go.mod` (11 CVE de stdlib alcanzables: net/http, http2, crypto/tls, os; govulncheck ya no reporta ninguna llamada). `npm audit fix` (source-map-js) y dependencias web menores al día (TypeScript 7 queda fuera: mayor).
- **react-doctor** 68 -> 70, sin errores: arreglados `useColumnVisibility` (efecto dentro del updater) y estado inicial perezoso en `ChannelFormDialog`. Falsos positivos anotados: secreto en `useLogin` (es un mensaje), `usePolledFetch` (ya aborta), `listAllDevices`/`listCollectors` (el cursor obliga a ir en serie), `SearchBox` (se reinicia por `key`), Recharts "eager" (ya está en su chunk). Los avisos de complejidad no se tocan.
- **UX**: parches del host sin la tabla de historial (repetía la gráfica); al día = una sola línea. "Perfiles SNMP" sale del sidebar y vive como sección en Configuración (la ruta `/perfiles-snmp` se quitó). Chequeos pausados visibles con badge "Pausado" (`check.enabled` en `GET /hosts/{id}/service-checks`).
- **Webhook**: el mensaje lleva `content` (Discord, tope 2000) y `allowed_mentions: {parse: []}`.
- **Auto-monitoreo por webhook**: descartado (las entregas cuelgan de `alerts`; exigiría migración). Cubre el aviso del Resumen + `/metrics`/`/health`.
- **Quitado del plan**: I2, I4 (sin código), B24, B26-B30 pasan a una línea de "aplazado". Lo cerrado del plan pasó a `archivo.md`.
- `README.md` en la raíz (desarrollo y producción); `web/README.md` de plantilla borrado; `docker-compose.yml` con la misma clave que la BD local (`argos`).
- Tests no escritos (ver `tests-pendientes.md`): store de chequeos pausados en `HostServiceChecks`.
