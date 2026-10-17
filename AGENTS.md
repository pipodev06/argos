# argos

Software de monitoreo (tipo Zabbix/PRTG/Nagios) — agente en host + servidor central con dashboard.

**Al iniciar una sesión nueva:** leer `docs/estado-del-proyecto.md` (una página: qué hay, qué sigue, cómo verificar y levantar en local). Del resto (`docs/decisiones.md` índice, `docs/deuda-tecnica.md`) abrir solo lo que toque la tarea. Verificar siempre con `ARGOS_TEST_DATABASE_URL` definida (ver ese documento).

## Decisiones de estilo/diseño congeladas

**Estilo visual: "Slate"** (elegido por el usuario el 2026-09-23, propuesto por ui-ux-designer; reemplaza a `terminal-dark`). Moderno y sereno, claro y oscuro. Cambiar cualquier punto de esta sección requiere pedido explícito del usuario.

**Modo:** claro y oscuro, claro por defecto. Se cambia SOLO con un botón (toggle manual); no se detecta la preferencia del sistema (`prefers-color-scheme`) — decidido por el usuario el 2026-09-23. La elección se recuerda en el navegador (preferencia por-viewer en `localStorage`, con try/catch); el estado del tema se aplica antes del primer render pa evitar parpadeo, sin scripts inline (CSP estricta).

**Tokens** (contrastes WCAG calculados por el diseñador, ±0.05):

| Token | Claro | Oscuro |
|---|---|---|
| bg | `#F8FAFC` | `#0B1220` |
| surface | `#FFFFFF` | `#111A2C` |
| border | `#E2E8F0` | `#1F2A3F` |
| text (primario) | `#0F172A` | `#E6EAF2` |
| text-muted (secundario) | `#526074` | `#94A3B8` |
| brand (solo acciones) | `#2563EB` (texto blanco encima) | `#60A5FA` |
| ok | `#15803D` | `#4ADE80` |
| warn | `#B45309` | `#FBBF24` |
| danger | `#B91C1C` | `#F87171` |
| info | `#0E7490` | `#22D3EE` |

**Reglas de color:**
- Texto: solo 2 tonos (`text` y `text-muted`), siempre ≥4.5:1.
- El color de marca (`brand`) es solo para acciones (botones primarios, links, foco). Nunca como estado.
- El color de estado (ok/warn/danger/info) va solo en icono, punto o relleno/tinte, **nunca en texto**. El estado nunca depende solo del color: siempre icono + texto.

**Tipografía:** Inter Variable auto-hospedada (`@fontsource-variable/inter`), sin CDN, con `tabular-nums` en datos numéricos. Mono (`ui-monospace`) solo para tokens de enroll y comandos copiables (`CopyBlock`); IP, ID y puntos de montaje usan la fuente normal (decidido por el usuario el 2026-09-24).

**Forma:** un solo radio, `6px` (`rounded-md`), en todo. Sin sombras en cards/filas (separación por borde 1px). Única excepción: sombra suave en modal y popover. El anillo de foco visible (accesibilidad) está permitido.

**Patrón fila de host:** fila compacta (celdas `px-2 py-1`, ~30px; antes 36px); nombre en semibold; IP/OS en `text-muted`; badge de estado = pill tintada con icono + texto; si el host está `down`, barra de 2px `danger` a la izquierda. Iconos de estado (lucide): up `CircleCheckIcon`, down `CircleXIcon`, pending `ClockIcon`, disabled `CircleOffIcon` (muted).

**Estados de host** (aprobado el 2026-09-23): `up` (`CircleCheckIcon`, ok), `down`/Caído (`CircleXIcon`, danger + barra 2px), `offline`/Sin conexión (`UnplugIcon`, muted, sin barra: es normal, no cuenta como falla ni entra en "Requieren atención"), `pending` (`ClockIcon`, info), `disabled` (`CircleOffIcon`, muted). El grupo decide (`always_on`) si un host que deja de reportar pasa a Caído o a Sin conexión; hosts sin grupo pasan a Sin conexión.

**Layout y navegación** (aprobado el 2026-09-23): sidebar de 240px colapsable a 56px (solo iconos con tooltip), drawer (Sheet) en móvil, ítems con `NavLink`; estado activo = `bg-muted` + texto semibold en `text` + barra de 2px a la izquierda en `text` (nunca `brand` ni color de estado). Contenido a ancho completo (sin tope de ancho; crece al contraer el sidebar), decidido por el usuario el 2026-09-24. Ítems desde un arreglo de config; sin ítems "próximamente". Rutas: `/` Resumen, `/hosts`, `/hosts/:id`, `/grupos`.
**Resumen** (decidido por el usuario el 2026-09-26): tres bloques alineados de 3 filas: **Hosts** (2 columnas × 3 filas: Activos/Caídos, Sin conexión/Pendientes y Deshabilitados a ancho completo), **Dispositivos** (mismo patrón; solo si hay dispositivos) y **Alertas abiertas · N** (3 tarjetas apiladas: Críticas, Advertencias, Info; cada una enlaza a `/alertas?state=open&severity=…` y la de críticas lleva la barra `danger` si hay alguna). En pantallas angostas los bloques se apilan. Un dispositivo puede deshabilitarse/habilitarse (acción en la fila y en el detalle, sin confirmación: es reversible).
**Tipografía en pantallas:** solo 4 tamaños: `xs` meta, `sm` cuerpo y encabezados de sección (semibold), `lg` título de página, `2xl` cifras grandes.
**Controles nuevos** (checkbox, radio, select): borde con `border-text-muted` (contraste ≥3:1); el `Input` existente no cambia.
**Patrones nuevos:** tabla semántica sobre la `Table` de shadcn (vía `DataTable`) con celdas compactas `px-2 py-1` y cabecera de 32px (pedido del usuario el 2026-10-17; antes filas de 36px), encabezados y celdas centrados horizontalmente en todas las tablas (decidido por el usuario el 2026-09-24), primera columna `#` con el número de fila (1, 2, 3… continuo entre páginas; va entre la casilla de selección, si la hay, y la columna del nombre; decidido por el usuario el 2026-09-25), encabezados ordenables con `aria-sort`, fila entera clicable vía enlace del nombre; filtros como chips (radio 6px, ancho fijo `w-40`, texto truncado) con estado en la URL; paginación por cursor con un único componente `Pagination` (tamaños de página 15, 30, 50 y 100, por defecto 15: decidido por el usuario el 2026-09-25); tile de contador (icono de estado + etiqueta `xs` muted + cifra `2xl` tabular); barra de uso (`UsageBar`, `role="meter"`, marcas en 80 % y 90 %, estado con icono + palabra, nunca solo color); umbrales de disco globales 80/90; `CopyBlock` para comandos copiables. Vista "pared" de hosts: diferida.

**Ajustes del 2026-10-17** (pedidos por el usuario; auditoría de ui-ux-designer):
- **Estado vacío:** `EmptyState` = icono + una sola línea muted (`Sin hosts`, `Sin datos`, `Sin permiso`…), sin textos de ayuda; filtros sin coincidencias = `NoResultsState` con el texto fijo "Sin resultados" + "Limpiar".
- **Ritmo vertical:** 16px (`gap-4`, y `mb-4` del `PageHeader`) entre bloques de una página (título, filtros, tabla, paginación, secciones); 8px (`gap-2`) dentro de un bloque. Sin `gap-3`/`gap-6` en contenedores de página.
- **Texto:** `xs` solo para etiquetas, ayudas, pills y contadores; los datos de celdas y filas van en `sm`.
- **Botón deshabilitado:** plano (`bg-muted` + `text-text-muted`), nunca `opacity` (el primario al 50% era ilegible en oscuro).
- **Sin header superior en escritorio** (la marca va en el sidebar y en la barra móvil).
- **Componentes shadcn, nunca controles nativos con popup del navegador:** fechas con `DateTimePicker`/`DateTimeField` (Popover + Calendar de shadcn, `components/ui/date-time-picker.tsx`; prohibido `type="date"`/`"datetime-local"`); pills de estado solo con `Badge` (`tint` ok/warn/danger/info/muted pinta fondo e icono, texto en `text`, radio 6px).
- **Acciones irreversibles** (eliminar, revocar, purgar, deshabilitar un agente): botón `variant="destructive"` (`bg-danger` + texto `bg`); en `ConfirmDialog`, prop `destructive`. Excepción a "brand solo en acciones": el rojo marca peligro, no estado.
- **Informes:** exportar en el `PageHeader`; filtros en una fila; Desde/Hasta en otra fila solo con "Personalizado".
- **Construido sobre shadcn** (pedido del usuario el 2026-10-17): layout con el `Sidebar` de shadcn (`collapsible="icon"`, 240/56px, drawer en móvil, estado en localStorage sin cookie); tarjetas con `Card` (adaptada: borde 1px, sin anillo; `StatCard`, `StatusTile`, `EmptyState`, gráficas, login y retención la usan); tablas con `Table`. Antes de crear un componente, `pnpm dlx shadcn@latest add <x>` y adaptarlo a Slate.
- **Controles compactos:** botón `h-8 px-2` (sm `h-7 px-2`, icono `size-8`/`size-7`), input y select `h-8 px-2`, pestañas y segmentos `px-2 py-1`.
- **Logo:** `AppLogo` = marca (ojo atravesado por una línea de pulso, en cuadro invertido `bg-text`/`text-bg`) + "argos" en `lg` semibold; `public/favicon.svg` y `docs/assets/logo.svg` (README) con el mismo dibujo.
- **Grupos:** una sola acción "Eliminar": grupo vacío se borra; con hosts abre la baja (purga) con "Eliminar también el grupo" marcado.

**Gráficas (paleta categórica de 5 series, basada en Okabe-Ito):**
- Claro: `#0072B2`, `#D55E00`, `#007A5C`, `#A66A00`, `#A6427F`.
- Oscuro: `#56B4E9`, `#E69F00`, `#009E73`, `#F0E442`, `#CC79A7`.
- Diferenciar además por trazo (línea, punteada, punto-raya) y leyenda directa; una métrica por gráfica, usar 5 series solo pa multi-núcleo/multi-host. No se corrió simulación de daltonismo (CVD).

## Stack

Vite + Tailwind v4 (`@tailwindcss/vite`) + shadcn/ui (radix) + **lucide-react** (desde el 2026-10-17, pedido del usuario; reemplaza a Tabler): nombres con sufijo `Icon` (`CalendarIcon`) para no chocar con componentes, `strokeWidth={1.5}`, tipo `LucideIcon`; sin otras librerías de iconos. Gestor de paquetes: **pnpm**. Gráficas: shadcn chart (Recharts) para MVP; evaluar uPlot si el volumen de puntos por host lo exige.
