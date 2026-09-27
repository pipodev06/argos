-- +goose Up
-- Detalle comprimido de disk_samples (paquete internal/chunk, codec propio de disco: internal/chunk/disk.go),
-- espejo de metrics_chunks (migracion 00033) pero mas simple: sin retencion por host/grupo (disk_samples solo
-- tiene la retencion global ARGOS_DISK_RETENTION_DAYS) y sin marca de agua de resumen (no hay rollup de disco).
-- El mantenimiento empaqueta cada host-dia antes de borrar su particion cruda, con una ventana caliente fija
-- (internal/retention.diskHotDays, no configurable) mas corta que la retencion total: la cruda vive esa ventana
-- y el chunk se conserva hasta ARGOS_DISK_RETENTION_DAYS.
SET LOCAL lock_timeout = '5s';

CREATE TABLE disk_chunks (
  host_id     UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  day         DATE NOT NULL,                       -- dia UTC
  mount_count SMALLINT NOT NULL CHECK (mount_count >= 0),  -- montajes distintos del host ese dia
  n           INT NOT NULL CHECK (n >= 0),         -- muestras del chunk (todos los montajes)
  codec       SMALLINT NOT NULL,                   -- version del formato (chunk.DiskVersion)
  data        BYTEA NOT NULL,                      -- ya viene comprimido (deflate): sin recompresion de TOAST
  packed_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (host_id, day)
);
ALTER TABLE disk_chunks ALTER COLUMN data SET STORAGE EXTERNAL;

-- +goose Down
DROP TABLE disk_chunks;
