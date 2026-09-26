-- +goose Up
-- Detalle comprimido por host y dia (paquete internal/chunk) y retencion del detalle por host, grupo y global.
-- metrics (particiones diarias) queda como ventana caliente; al vencer, el mantenimiento empaqueta cada host-dia en un
-- chunk (~8 B por muestra contra ~178 B de la fila) antes de borrar la particion. El detalle se conserva
-- host ?? grupo ?? global dias (8..730) y el mantenimiento borra los chunks vencidos.
SET LOCAL lock_timeout = '5s';

CREATE TABLE metrics_chunks (
  host_id   UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  day       DATE NOT NULL,                       -- dia UTC
  n         INT NOT NULL CHECK (n >= 0),         -- muestras del chunk
  codec     SMALLINT NOT NULL,                   -- version del formato (chunk.Version)
  data      BYTEA NOT NULL,                      -- ya viene comprimido (deflate): sin recompresion de TOAST
  packed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (host_id, day)
);
ALTER TABLE metrics_chunks ALTER COLUMN data SET STORAGE EXTERNAL;

-- Ajustes globales editables desde la API (sin reiniciar). Clave conocida: metrics_detail_days (numero JSON).
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by UUID REFERENCES users(id) ON DELETE SET NULL
);

-- NULL = hereda del grupo o del global.
ALTER TABLE host_groups ADD COLUMN detail_retention_days INT
  CONSTRAINT host_groups_detail_retention_check CHECK (detail_retention_days BETWEEN 8 AND 730);
ALTER TABLE hosts ADD COLUMN detail_retention_days INT
  CONSTRAINT hosts_detail_retention_check CHECK (detail_retention_days BETWEEN 8 AND 730);

-- +goose Down
ALTER TABLE hosts DROP COLUMN detail_retention_days;
ALTER TABLE host_groups DROP COLUMN detail_retention_days;
DROP TABLE settings;
DROP TABLE metrics_chunks;
