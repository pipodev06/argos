# Mapa de responsabilidades: tipo de problema -> agente

| Tipo de problema | Agente |
|---|---|
| Handlers, rutas, auth, sesiones, ingest, detector, contratos de API (`server/`) | backend-engineer |
| Agente (`agent/`): colectores, transporte, servicio del SO, instalador | backend-engineer |
| Esquema, migraciones, particionado, retencion, indices, rendimiento de consultas | data-infra-engineer |
| React/Vite/Tailwind/shadcn, hooks, polling, graficas, rutas (`web/`) | frontend-engineer |
| Estilo Slate, layout, jerarquia visual, accesibilidad, estados vacio/error | ui-ux-designer |
| Tests, cobertura, e2e, regresiones, verificacion contra Postgres real | qa-testing-engineer |
| CI (`-race`), Dockerfiles, empaquetado, firma de binarios, despliegue, proxy/TLS | devops-engineer |
| Amenazas, hardening, revision de auth/servicio | security-engineer |
| Estado, decisiones, deuda, coherencia de `docs/` | orquestador (Claude principal) |
| Evaluar librerias/herramientas nuevas | orquestador, con el usuario (WebSearch), o security-engineer si es de seguridad |

Reglas: los agentes no escriben en `docs/`; devuelven "notas para docs" y el orquestador las vuelca. Leer `docs/decisiones.md` (indice) primero y abrir solo los temas de la tarea.
