# Frontend: datos, navegacion y graficas (2026-09-23)
Sin probar e2e con la API real ni en navegador. Login/sesion: `auth-y-sesiones.md`. Estilo/componentes: `frontend-estilo-y-componentes.md`.

## Datos y estado
- `web/src/lib/api.ts` es el unico punto de HTTP (fetch + envelope `{data,error}` -> `ApiError`). Sin Axios (migrar si aparecen interceptores mas complejos). Sin Zod/react-hook-form (formularios de 1-3 campos), sin Zustand (React Context / `useSyncExternalStore`); migrar cuando otra feature comparta estado global.
- Form de hostname: un input con validacion `trim()` no vacio.
- Dev: `server.proxy` `/api` -> `http://localhost:8080` en `vite.config.ts` (el server Go no tiene CORS).
- **Polling compartido** `hooks/usePolledFetch.ts` (AbortController, pausa con pestana oculta y refresca al volver, se detiene ante 401 y ante 400/404, descarta datos de host/rango anterior). `useHosts` (10 s), `useHostDetail` y `useHostMetrics` (30 s; 5 min en rangos largos) lo usan. Los pollings mueren con el desmontaje de la pagina.
- Sin runner de tests en `web/` (solo `build` y `lint`); helpers puros verificados con scripts temporales. Vitest candidato (ver indice).

## Navegacion (react-router)
- Dependencia aprobada: SOLO `react-router` **8.4.0** (paquete unico, no `react-router-dom`; node >= 22.22, react >= 19.2.7). **Modo declarativo** con `<BrowserRouter>` en `main.tsx`, `<Routes>/<Route>` en `DashboardPage` (3 rutas, sin loaders; migrar a data mode si hacen falta loaders/`ScrollRestoration`).
- Rutas: `/` (redirige a `/hosts`), `/hosts` (`HostsPage`), `/hosts/:id` (`HostDetailPage`), `*` (`NotFoundPage`). `AuthGate` envuelve todo: sin sesion login (la URL pedida se conserva y se muestra tras el login).
- `HostDetailPage` valida `:id` con `isHostId` (UUID, `features/hosts/utils/id.ts`); si no, "Id de host invalido." sin llamar a la API. `key={id}` reinicia el estado. 400/404 de la API = "Host no encontrado.". `HostRow` y "Hosts" del detalle usan `<Link>`; un solo `BackToHostsLink`. Sin `useNavigate`. Scroll al inicio al cambiar `pathname` (efecto en `DashboardPage`). Tema y menu de usuario en el header de `DashboardPage`, visibles en todas las rutas.
- "Agregar host" (solo admin) en la fila del titulo "Hosts" de `HostsPage`.

## Detalle de host con graficas
- **Recharts via shadcn `chart`, recortado**: `components/ui/chart.tsx` conserva `ChartContainer`, `ChartTooltip`, `ChartTooltipContent` (+ `LineSwatch`). Eliminados `ChartStyle` (`<style>`, bloqueado por CSP), `config` por tema, leyenda de shadcn y tokens oklch/sombras. Colores `var(--chart-N)` directo en cada serie. Bundle ~660 kB (Recharts): evaluar `import()` del detalle o uPlot.
- Paleta: CPU y Red-bajada `chart-1` (solida), Memoria `chart-3`, Red-subida `chart-2` punteada (`6 4`), memoria total = linea de referencia `text-muted` punto-raya (`6 3 1 3`) con dominio +8 % de margen. Leyenda por grafica con muestra de trazo (color + patron).
- **Huecos como huecos**: eje X numerico de tiempo real con ventana fija [fin - rango, fin]. `buildRows` inserta fila vacia si entre dos puntos falta > 1,5 pasos (`step_seconds`); series `connectNulls={false}`; `net_*_bps` null tambien cortan. Puntos aislados como circulo. Interpolacion `linear`, sin animacion.
- Unidades: memoria base 1024 (MB -> GB), red base 1000 (B/s, KB/s, MB/s, GB/s). Horas locales 24 h `es-PE`.
- **Rangos** `1h|6h|24h|7d|30d|90d|1y` (`MetricRange`; "1a" rotula 1 ano, `aria-label`/`title` "Ultimo ano"). Cada `RangeConfig` (`utils/chart.ts`) lleva `ariaLabel` y `refreshMs` (30 s pa 1h..7d; 5 min pa 30d/90d/1y: vienen de resumenes por hora). Marcas del eje X alineadas a hora/medianoche local: 1h:10 min, 6h:1 h, 24h:4 h, 7d:1 dia, 30d:5 dias, 90d:15, 1y:60 (fecha "25 ago."; 1y con ano). Tooltip (`formatTooltipLabel`, `formatTick`): 30d fecha + hora sin segundos, 90d fecha, 1y fecha con ano; con pasos de 3 h/12 h no distingue puntos del mismo dia.
- `SegmentedControl` unico selector: `ariaLabel`, `max-w-full overflow-x-auto` (7 opciones en movil hacen scroll interno), botones `shrink-0`, radiogroup con flechas/Home/End, foco `brand`, activo por fondo + peso.
- Accesibilidad: cada grafica `role="img"` + `aria-label` con ultimo/min/max; `accessibilityLayer={false}`; las tarjetas de ultimo valor son la alternativa textual. Tooltip solo raton/tactil.
- `MetricChartCard`: altura fija 224 px, estados cargando/vacio/error. Vacio = "Sin datos en este rango." (host nuevo o hora en curso sin resumir). Error: 400/404 -> "Host no encontrado." sin graficas; otro -> `InlineError` conservando los ultimos datos; un 400 `invalid_range` sale por `InlineError` y el polling se detiene (`isClientError`) hasta cambiar de rango. Utilidades puras en `features/hosts/utils/{format,chart}.ts`.
