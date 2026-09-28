-- +goose Up
-- Bajas en segundo plano: la peticion HTTP valida y encola un trabajo; un worker del server lo ejecuta y el cliente
-- consulta su progreso. target_id no tiene FK: el destino desaparece al terminar. requested_by_name conserva el actor
-- aunque el usuario se borre.
CREATE TABLE purge_jobs (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind              text NOT NULL CHECK (kind IN ('host', 'device', 'group')),
  target_id         uuid NOT NULL,
  target_name       text NOT NULL,
  options           jsonb NOT NULL DEFAULT '{}',
  requested_by      uuid REFERENCES users(id) ON DELETE SET NULL,
  requested_by_name text NOT NULL,
  state             text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'running', 'done', 'failed')),
  total_hosts       int NOT NULL DEFAULT 1 CHECK (total_hosts >= 0),
  done_hosts        int NOT NULL DEFAULT 0 CHECK (done_hosts >= 0),
  result            jsonb,
  error             text,
  created_at        timestamptz NOT NULL DEFAULT now(),
  started_at        timestamptz,
  finished_at       timestamptz,
  heartbeat_at      timestamptz
);

-- Un solo trabajo activo por destino.
CREATE UNIQUE INDEX purge_jobs_active_target ON purge_jobs (kind, target_id) WHERE state IN ('queued', 'running');
CREATE INDEX purge_jobs_created ON purge_jobs (created_at DESC);

-- +goose Down
DROP TABLE purge_jobs;
