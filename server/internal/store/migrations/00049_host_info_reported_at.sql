-- +goose Up
-- Instante (ts del reporte) del ultimo reporte con host_info aplicado. Un reporte mas viejo no pisa la info: el agente
-- envia primero el reporte en vivo y despues drena el buffer offline, asi que llegan fuera de orden. NULL = fila
-- anterior a esta migracion (cualquier reporte la pisa).
ALTER TABLE host_info ADD COLUMN reported_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE host_info DROP COLUMN reported_at;
