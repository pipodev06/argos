# CSP, cabeceras y `<style>` inyectados (2026-09-23)
Ninguna de las verificaciones de CSP se probo en navegador real; todo es lectura de codigo.

## Cabeceras del server
- `Cache-Control: no-store` en `/api`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, CSP `default-src 'self'; frame-ancestors 'none'`, HSTS (`max-age=31536000`, sin includeSubDomains) solo si `ARGOS_PUBLIC_URL` es https.
- Allowlist de `Host` (`ARGOS_PUBLIC_URL` + localhost/127.0.0.1/::1, sin importar el puerto) -> `421 invalid_host`; se exceptuan `/health` y `/api/v1/agent/*` (clientes no navegador, sin cookies, pueden llegar por IP).
- Al servir el SPA la CSP debe permitir los assets del build (scripts, CSS y fuentes propios en `'self'`).

## Build del frontend bajo esa CSP
- Revisado: index.html sin scripts/estilos inline, fuentes Inter como woff2 propios, sin `data:` URIs, sin eval/`new Function`, sin recursos de terceros. Tema inicial sin script inline (`initTheme()` en `main.tsx`).
- Recharts 3.8.0 (es6): no inyecta `<style>`/`insertRule`; solo atributos `style` via React (CSSOM, validos con la CSP). Por eso se elimino `ChartStyle` de shadcn `chart` (ver `frontend-datos-y-navegacion.md`).
- Radix: inyectan `<style>` (a) `react-remove-scroll` en Dialog/AlertDialog/Sheet/Select y en Popover/DropdownMenu **solo si son modales**; (b) `Select` un `<style>` propio en su Viewport (oculta la barra de scroll nativa). No inyectan: Tooltip, Tabs, Checkbox, Radio, Collapsible, Label; los `style` de Popper/UsageBar/FocusGuards van por CSSOM (permitido).
- Efecto: el modal/drawer funciona (focus trap y Esc no dependen del `<style>`) pero el scroll del fondo puede no bloquearse; `Select` (sin uso aun) muestra barra de scroll nativa (cosmetico).
- Contencion: Popover y DropdownMenu quedan no modales (`modal={false}` obligatorio para DropdownMenu, documentado en el componente); `Pagination` usa `SegmentedControl` en vez de `Select` (evita su `<style>`).
- Decision pendiente del usuario: aceptar la degradacion o `style-src 'self' 'nonce-...'` con nonce por request (requiere inyeccion en index.html / `__webpack_nonce__`).

## SPA con BrowserRouter en produccion (no implementado)
- Al servir el SPA desde el binario Go, responder `index.html` como fallback en toda ruta que no sea `/api/*` ni archivo estatico existente (`/hosts/<uuid>` al recargar, cualquier ruta -> pagina 404 del SPA). `/api/*` desconocido sigue siendo 404 JSON. `index.html` sin cache larga; assets con hash con cache larga. En dev Vite ya hace el fallback y el proxy `/api` no choca.
