-- +goose Up
-- Historial de transiciones de status de un host. Lo escriben, en la MISMA sentencia que cambia hosts.status
-- (CTE UPDATE ... RETURNING -> INSERT): el detector (up -> down/offline), el ingest (-> up), el cambio de grupo /
-- always_on (down <-> offline) y revocar/deshabilitar (-> disabled). Retencion configurable (default 180 dias).
CREATE TABLE host_events (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  from_status TEXT NOT NULL CHECK (from_status IN ('pending', 'up', 'down', 'offline', 'disabled')),
  to_status TEXT NOT NULL CHECK (to_status IN ('pending', 'up', 'down', 'offline', 'disabled')),
  at TIMESTAMPTZ NOT NULL DEFAULT now(),
  source TEXT NOT NULL CHECK (source IN ('detector', 'ingest', 'admin', 'group_change')),
  CHECK (from_status <> to_status)
);
CREATE INDEX host_events_host_at_idx ON host_events (host_id, at DESC, id DESC);
CREATE INDEX host_events_at_idx ON host_events (at);

-- +goose Down
DROP TABLE host_events;
