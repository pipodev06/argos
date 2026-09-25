-- +goose Up
-- Inventario del host (puertos, servicios, procesos): ultima foto por (host, modulo) e historial de cambios.
-- Solo puertos y servicios tienen historial; procesos es ruido y no lo tiene.

CREATE TABLE inventory_current (
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  module TEXT NOT NULL CHECK (module IN ('ports','services','processes')),
  rev TEXT NOT NULL CHECK (rev ~ '^[0-9a-f]{64}$'),
  items JSONB NOT NULL CHECK (jsonb_typeof(items) = 'array'),
  item_count INT NOT NULL CHECK (item_count BETWEEN 0 AND 1024),
  truncated BOOLEAN NOT NULL DEFAULT false,
  collected_at TIMESTAMPTZ NOT NULL,
  received_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (host_id, module)
);

CREATE TABLE inventory_changes (
  id BIGSERIAL PRIMARY KEY,
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  module TEXT NOT NULL CHECK (module IN ('ports','services')),
  kind TEXT NOT NULL CHECK (kind IN ('added','removed','changed','overflow')),
  key TEXT NOT NULL CHECK (char_length(key) <= 200),
  before JSONB,
  after JSONB,
  at TIMESTAMPTZ NOT NULL
);
CREATE INDEX inventory_changes_host_at_idx ON inventory_changes (host_id, at DESC, id DESC);
CREATE INDEX inventory_changes_module_kind_at_idx ON inventory_changes (module, kind, at);

-- +goose Down
DROP TABLE inventory_changes;
DROP TABLE inventory_current;
