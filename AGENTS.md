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

**Tipografía:** Geist Variable auto-hospedada (`@fontsource-variable/geist`; reemplaza a Inter el 2026-10-17, pedido del usuario), sin CDN, con `tabular-nums` en datos numéricos. Mono (`ui-monospace`) solo para tokens de enroll y comandos copiables (`CopyBlock`); IP, ID y puntos de montaje usan la fuente normal (decidido por el usuario el 2026-09-24).

**Forma:** un solo radio, `6px` (`rounded-md`), en todo. Sin sombras en cards/filas (separación por borde 1px). Única excepción: sombra suave en modal y popover. El anillo de foco visible (accesibilidad) está permitido.

**Patrón fila de host:** fila compacta (celdas `px-2 py-1`, ~30px; antes 36px); nombre en semibold; IP/OS en `text-muted`; badge de estado = pill tintada con icono + texto; si el host está `down`, barra de 2px `danger` a la izquierda. Iconos de estado (lucide): up `CircleCheckIcon`, down `CircleXIcon`, pending `ClockIcon`, disabled `CircleOffIcon` (muted).

**Estados de host** (aprobado el 2026-09-23): `up` (`CircleCheckIcon`, ok), `down`/Caído (`CircleXIcon`, danger + barra 2px), `offline`/Sin conexión (`UnplugIcon`, muted, sin barra: es normal, no cuenta como falla ni entra en "Requieren atención"), `pending` (`ClockIcon`, info), `disabled` (`CircleOffIcon`, muted). El grupo decide (`always_on`) si un host que deja de reportar pasa a Caído o a Sin conexión; hosts sin grupo pasan a Sin conexión.

**Layout y navegación** (aprobado el 2026-09-23): sidebar de 240px colapsable a 56px (solo iconos con tooltip), drawer (Sheet) en móvil, ítems con `NavLink`; estado activo = `bg-muted` + texto semibold en `text` + barra de 2px a la izquierda en `text` (nunca `brand` ni color de estado). Contenido a ancho completo (sin tope de ancho; crece al contraer el sidebar), decidido por el usuario el 2026-09-24. Ítems desde un arreglo de config; sin ítems "próximamente".
**Navegación y rutas** (aprobado por el usuario el 2026-10-18; reemplaza a "Gestión" con submenús): menú plano en tres secciones. **Monitoreo** = Resumen `/`, Alertas `/alertas` (con contador), Hosts `/hosts`, Dispositivos `/dispositivos`, Parches `/parches`, Informes `/informes`. **Configuración** = Grupos `/grupos`, Chequeos `/chequeos`, Reglas `/alertas/reglas`, Mantenimiento `/alertas/mantenimiento`, Canales `/alertas/canales`, Perfiles SNMP `/perfiles-snmp`. **Administración** = Usuarios, Sistema `/sistema` (antes "Configuración"; `/configuracion` redirige), Auditoría. Un dispositivo tiene ruta propia `/dispositivos/:id` (nunca `/hosts/:id`). El host usa layout anidado (cabecera fija + sub-rutas): `/hosts/:id` Resumen, `/metricas` (con rango personalizado y CSV), `/chequeos`, `/dispositivos` (solo colectores), `/parches`, `/inventario`, `/historial`. Migas con el nombre real (`useBreadcrumbTrail`).
**Resumen** (decidido por el usuario el 2026-09-26): tres bloques alineados de 3 filas: **Hosts** (2 columnas × 3 filas: Activos/Caídos, Sin conexión/Pendientes y Deshabilitados a ancho completo), **Dispositivos** (mismo patrón; solo si hay dispositivos) y **Alertas abiertas · N** (3 tarjetas apiladas: Críticas, Advertencias, Info; cada una enlaza a `/alertas?state=open&severity=…` y la de críticas lleva la barra `danger` si hay alguna). En pantallas angostas los bloques se apilan. Un dispositivo puede deshabilitarse/habilitarse (acción en la fila y en el detalle, sin confirmación: es reversible).
**Tipografía en pantallas:** solo 4 tamaños: `xs` 12px meta, `sm` 13px cuerpo y encabezados de sección (semibold), `lg` 16px título de página, `2xl` 20px cifras grandes (un nivel abajo de Tailwind, fijado en los tokens de `index.css` el 2026-10-17).
**Controles nuevos** (checkbox, radio, select): borde con `border-text-muted` (contraste ≥3:1); el `Input` existente no cambia.
**Patrones nuevos:** tabla semántica sobre la `Table` de shadcn (vía `DataTable`) con celdas compactas `px-2 py-1` y cabecera de 32px (pedido del usuario el 2026-10-17; antes filas de 36px), encabezados y celdas centrados horizontalmente en todas las tablas (decidido por el usuario el 2026-09-24), primera columna `#` con el número de fila (1, 2, 3… continuo entre páginas; va entre la casilla de selección, si la hay, y la columna del nombre; decidido por el usuario el 2026-09-25), encabezados ordenables con `aria-sort`, fila entera clicable vía enlace del nombre; filtros en una sola fila con estado en la URL, patrón "faceted filter" de la tabla de shadcn (pedido del usuario el 2026-10-17; antes chips de ancho fijo `w-40`): búsqueda que llena el ancho libre de la fila, cada filtro es un botón `h-8` con borde punteado `+ Etiqueta` que muestra su valor dentro (quitar desde su popover), "Más filtros" con icono (numéricos y casillas on/off), fechas en un solo filtro de rango (`DateRangeFilter`: atajos Hoy/24 h/7/30 días + personalizado, sin días futuros), "Limpiar ×" solo si hay filtros y acciones (Columnas) a la derecha; sin línea de contador bajo los filtros (el total lo da el pie de la tabla); paginación por cursor con un único componente `Pagination` (tamaños de página 15, 30, 50 y 100, por defecto 15: decidido por el usuario el 2026-09-25); tile de contador (icono de estado + etiqueta `xs` muted + cifra `2xl` tabular); barra de uso (`UsageBar`, `role="meter"`, marcas en 80 % y 90 %, estado con icono + palabra, nunca solo color); umbrales de disco globales 80/90; `CopyBlock` para comandos copiables. Vista "pared" de hosts: diferida.

**Ajustes del 2026-10-17** (pedidos por el usuario; auditoría de ui-ux-designer):
- **Estado vacío:** `EmptyState` = icono + una sola línea muted (`Sin hosts`, `Sin datos`, `Sin permiso`…), sin textos de ayuda; filtros sin coincidencias = `NoResultsState` con el texto fijo "Sin resultados" + "Limpiar".
- **Ritmo vertical:** 12px (`gap-3`, y `mb-3` del `PageHeader`) entre bloques de una página (título, filtros, tabla, paginación, secciones; 16px hasta el 2026-10-17, bajado a pedido del usuario); 8px (`gap-2`) dentro de un bloque. Sin `gap-4`/`gap-6` en contenedores de página.
- **Sin texto de ayuda en formularios** (pedido del usuario el 2026-10-17, "ruido visual"): nada de descripciones bajo campos, casillas ni títulos de diálogo, y etiquetas cortas ("Modo privilegiado", no "Modo privilegiado (más visibilidad)"). La explicación de cada campo va en `FormHelp` (`components/ui/form-help.tsx`): icono de ayuda junto al título (`FormPage help` o en el `DialogHeader`) que abre un modal con campo → uso. Bajo un campo solo se muestran errores y datos vivos (contador de caracteres, "3 de 10 en uso"). Un secreto ya guardado se indica con el placeholder "Sin cambios". Alcance y su objetivo (grupo, host…) van lado a lado.
- **Texto:** `xs` solo para etiquetas, ayudas, pills y contadores; los datos de celdas y filas van en `sm`.
- **Botón deshabilitado:** plano (`bg-muted` + `text-text-muted`), nunca `opacity` (el primario al 50% era ilegible en oscuro).
- **Layout = bloque `sidebar-07` de shadcn** (pedido del usuario el 2026-10-17; secciones planas desde el 2026-10-18, ver "Navegación y rutas"): cabecera del sidebar con marca (logo + "argos" + "Monitoreo"); pie `NavUser` (avatar de iniciales, menú con clave, tema y cierre de sesión); `SidebarRail`; barra superior de 48px en el contenido con `SidebarTrigger` + separador + migas (sección › página › Detalle).
- **Componentes shadcn, nunca controles nativos con popup del navegador:** fechas con `DateTimePicker`/`DateTimeField` (Popover + Calendar de shadcn, `components/ui/date-time-picker.tsx`; prohibido `type="date"`/`"datetime-local"`); pills de estado solo con `Badge` (`tint` ok/warn/danger/info/muted pinta fondo e icono, texto en `text`, radio 6px).
- **Acciones irreversibles** (eliminar, revocar, purgar, deshabilitar un agente): botón `variant="destructive"` (`bg-danger` + texto `bg`); en `ConfirmDialog`, prop `destructive`. Excepción a "brand solo en acciones": el rojo marca peligro, no estado.
- **Informes:** exportar en el `PageHeader`; filtros en una fila, Desde/Hasta en la misma fila solo con "Personalizado" (sin días futuros; el rango se valida antes de pedirlo).
- **Acciones por fila y navegación** (pedido del usuario el 2026-10-17): nada de nombres-enlace ni filas clicables. Toda tabla lleva al final la columna con encabezado visible "Acciones" (`ActionsHeadCell`/`ActionsCell`, `components/data-table/RowActions.tsx`) con botones de icono `ghost`: ojo = ver detalle (`ViewAction`), lápiz = editar, papelera = eliminar. Fuera de tablas (resumen, detalles), ir a otra pantalla = botón `outline` `sm` o el ojo junto al nombre, nunca texto subrayado. Volver = `BackLink`.
- **Detalle de host** (pedido del usuario el 2026-10-18): cabecera (nombre, estado, acciones de `hostActions`) y debajo pestañas como sub-rutas (`HostLayout`/`HostNav`, ver "Navegación y rutas"): Resumen (tarjetas actuales, alertas abiertas, sistema, discos; por defecto), Métricas, Chequeos (+ dispositivos que sondea), Parches (4 tarjetas fijas, línea de contexto, tabla de avisos con enlace al sitio oficial, evolución solo si hubo actividad), Inventario e Historial de estado. Solo se monta la pestaña activa.
- **Formularios en página** (`FormPage`): siempre a todo el ancho; grilla `items-start` de 2 columnas en `xl` (reglas: 4), filas completas con `xl:col-span-2`.
- **Valores por defecto sin etiqueta:** no se anota "(global)", "(opcional)", "(recomendado)", "(normal)": se muestra el valor efectivo y nada más.
- **Construido sobre shadcn** (pedido del usuario el 2026-10-17): layout con el `Sidebar` de shadcn (`collapsible="icon"`, 240/56px, drawer en móvil, estado en localStorage sin cookie); tarjetas con `Card` (adaptada: borde 1px, sin anillo; `StatCard`, `StatusTile`, `EmptyState`, gráficas, login y retención la usan); tablas con `Table`. Antes de crear un componente, `pnpm dlx shadcn@latest add <x>` y adaptarlo a Slate.
- **Espaciado compacto** (pedido del usuario el 2026-10-17): contenido `px-4 py-4` (`md:px-6`); `Card` con relleno de 12px (`--card-spacing` 3); campo = etiqueta + control a 4px (`gap-1`); formularios `gap-y-3` (`gap-x-4` entre columnas), alineados arriba (`items-start`). Formularios largos en página propia (`FormPage`); el de reglas usa todo el ancho (`wide`) en 2 columnas, 4 en `xl`.
- **Controles compactos:** botón `h-8 px-2` (sm `h-7 px-2`, icono `size-8`/`size-7`), input y select `h-8 px-2`, pestañas y segmentos `px-2 py-1`.
- **Logo:** `AppLogo` = marca (ojo atravesado por una línea de pulso, en cuadro invertido `bg-text`/`text-bg`) + "argos" en `lg` semibold; `public/favicon.svg` y `docs/assets/logo.svg` (README) con el mismo dibujo.
- **Grupos:** una sola acción "Eliminar": grupo vacío se borra; con hosts abre la baja (purga) con "Eliminar también el grupo" marcado.

**Gráficas (paleta categórica de 5 series, basada en Okabe-Ito):**
- Claro: `#0072B2`, `#D55E00`, `#007A5C`, `#A66A00`, `#A6427F`.
- Oscuro: `#56B4E9`, `#E69F00`, `#009E73`, `#F0E442`, `#CC79A7`.
- Diferenciar además por trazo (línea, punteada, punto-raya) y leyenda directa; una métrica por gráfica, usar 5 series solo pa multi-núcleo/multi-host. No se corrió simulación de daltonismo (CVD).

## Stack

Vite + Tailwind v4 (`@tailwindcss/vite`) + shadcn/ui (radix) + **lucide-react** (desde el 2026-10-17, pedido del usuario; reemplaza a Tabler): nombres con sufijo `Icon` (`CalendarIcon`) para no chocar con componentes, `strokeWidth={1.5}`, tipo `LucideIcon`; sin otras librerías de iconos. Gestor de paquetes: **pnpm**. Gráficas: shadcn chart (Recharts) para MVP; evaluar uPlot si el volumen de puntos por host lo exige.

## Datos técnicos para agentes (movidos desde `~/.claude/agents/` el 2026-10-27)

- **Verificación:** Go = `bash scripts/check-go.sh <server|agent> --fast [--pkg ./ruta]` mientras trabajas y completo al cerrar (gofmt, vet y golangci-lint también con `GOOS=linux`/`darwin`, tests, govulncheck; log completo en `.check/`). Web = `bash scripts/check-web.sh`. Tests de BD: definir `ARGOS_TEST_DATABASE_URL` (BD que termine en `_test`; la local está en `docs/deuda-tecnica.md`, sección Operación); sin ella se saltan en silencio.
- **Esquema real:** `grep -n "CREATE TABLE\|ALTER TABLE" server/internal/store/migrations/*.sql` (ej.: `host_events` existe desde la 00014).
- **Límites:** `maxSmallBodyBytes` = 4 KiB (no caben 200 UUID). Porcentajes de informes en 0–100.
- **Componentes web que ya existen:** `StatCard`/`StatusTile`, `DataTable`, `SegmentedControl` (elección de UNA opción), `Pagination`, `EmptyState`/`NoResultsState`, `InlineError`, `FormHelp`, `CopyBlock`, `DateTimePicker`, `RowActions`.
- **Greps antes de cerrar web:** `grep -rn "font-mono" web/src` solo debe dar `copy-block.tsx`; sin `text-right`/`text-left` en celdas; sin colores de estado (`text-danger`, `text-ok`…) en texto.
