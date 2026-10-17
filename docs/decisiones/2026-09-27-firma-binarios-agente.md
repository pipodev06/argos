# H7: firma de binarios del agente (2026-09-27)

> **2026-10-17:** quitado del código por decisión del usuario (sin despliegue, solo desarrollo). Recuperar del historial de git (commit `a75145e`) si vuelve la producción. La rotación de `ARGOS_SECRET_KEY` sigue vigente.

## Problema
`agent/internal/updater` descargaba `manifest.json` del mismo server y verificaba el sha256 del binario contra ese manifiesto. El sha256 protege de corrupción, no de un server (o túnel) comprometido: quien lo controla puede servir un manifiesto y un binario maliciosos a la vez.

## Decisión: ed25519 propio, no `cosign`
`cosign`/Sigstore está pensado para imágenes de contenedor con OIDC/Fulcio/Rekor en línea — no aplica bien a un self-hosted de un solo admin sin esa infraestructura. Un par de claves ed25519 (`server/internal/agentsig`, `agent/internal/agentsig`) da la misma garantía real (la clave privada nunca vive en el server desplegado) con muchas menos piezas.

## Cómo queda
- `argos-server keygen <archivo>` genera el par (clave privada 0600 en el archivo, pública impresa). `argos-server pubkey <archivo>` la deriva de nuevo. `argos-server sign-manifest <manifest.json> <archivo>` firma la forma **canónica** del manifiesto (`agentdist.CanonicalManifest`, la misma que sirve `/dl/latest/manifest.json`) y escribe `manifest.json.sig`, imprimiendo también el sha256 de la firma para guardarlo fuera de banda.
- La clave pública se compila DENTRO del agente (`-ldflags -X main.publicKey=<hex>`, misma técnica que `version`). `scripts/build-agent.sh` acepta `--key <archivo>`: compila con la clave pública embebida y firma el manifiesto en un solo paso. Sin `--key` el agente compila igual pero sin clave pública (solo desarrollo local).
- `agent/internal/updater` descarga también `manifest.json.sig` y verifica la firma con la clave pública embebida ANTES de decodificar el JSON. Sin clave embebida (`ErrNoPublicKey`) o firma inválida (`ErrBadSignature`), `update` falla cerrado — no se aplica ningún binario.
- El server sirve `GET /dl/latest/manifest.json.sig` tal cual; nunca tiene la clave privada ni valida la firma él mismo.
- La instalación inicial (`install`/`enroll`) no verifica firma (el binario ya lo trae el instalador que descargó el admin); solo `update`/`install.sh --update` la exigen.

## Deuda que queda
- `scripts/package-server.sh` no está atado a la firma (sigue empaquetando el server; la firma es del lado del agente en `build-agent.sh`, ya resuelto).
- No hay rotación de ESTA clave de firma todavía (si se pierde o se filtra, hay que generar una nueva y todo agente existente queda sin poder `update` hasta reinstalarse con el binario nuevo). No es la misma clave que `ARGOS_SECRET_KEY` (esa sí rota, ver más abajo).
- Instrucciones para el admin: `deploy/README.md`, sección "Firma de binarios del agente (H7)".

## B18 (relacionado, mismo lote de agentes): rotación de `ARGOS_SECRET_KEY`
Antes, cambiar `ARGOS_SECRET_KEY` hacía perder el acceso a todo lo cifrado (canales, chequeos con secreto, perfiles SNMP): `CheckSecretKey` fallaba el arranque si la clave no coincidía con `secret_key_id`.

- `ARGOS_SECRET_KEY_PREVIOUS` (opcional, una o varias separadas por coma): forma un anillo de claves. La actual cifra y descifra; las previas solo descifran.
- `argos-server rotate-secret-key`: recifra canales, chequeos con secreto y perfiles SNMP con la clave actual (fila por fila, idempotente; repetirlo no toca lo ya recifrado). Deja una entrada en `audit_log` con el conteo.
- Paso a paso: (1) pon la clave vieja en `ARGOS_SECRET_KEY_PREVIOUS` y la nueva en `ARGOS_SECRET_KEY`; (2) reinicia el server (sigue leyendo con la vieja, ya cifra con la nueva); (3) corre `argos-server rotate-secret-key`; (4) verifica el resumen y `audit_log`; (5) quita `ARGOS_SECRET_KEY_PREVIOUS` y reinicia.
