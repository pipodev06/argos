-- +goose Up
-- B16: un enroll token puede ser "de grupo" (hostname NULL): reusable hasta max_uses veces, y cada
-- canje toma el hostname que reporta el propio agente en vez de uno fijo elegido por el admin.
-- max_uses/uses_count en 1/0 dejan el alta normal de hoy sin cambios de comportamiento.
ALTER TABLE enroll_tokens
  ALTER COLUMN hostname DROP NOT NULL,
  ADD COLUMN max_uses INT NOT NULL DEFAULT 1 CHECK (max_uses BETWEEN 1 AND 500),
  ADD COLUMN uses_count INT NOT NULL DEFAULT 0 CHECK (uses_count >= 0),
  -- Un token de grupo (sin hostname fijo) necesita un group_id: si no, EnrollHost no sabria en que
  -- grupo dejar los hosts que va creando.
  ADD CONSTRAINT enroll_tokens_group_token_needs_group CHECK (hostname IS NOT NULL OR group_id IS NOT NULL);

-- +goose Down
ALTER TABLE enroll_tokens
  DROP CONSTRAINT enroll_tokens_group_token_needs_group,
  DROP COLUMN uses_count,
  DROP COLUMN max_uses,
  ALTER COLUMN hostname SET NOT NULL;
