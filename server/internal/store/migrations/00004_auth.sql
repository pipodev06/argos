-- +goose Up
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username TEXT NOT NULL CHECK (char_length(username) BETWEEN 3 AND 64 AND username = btrim(username)),
  password_hash TEXT NOT NULL CHECK (starts_with(password_hash, '$argon2id$') AND char_length(password_hash) <= 512),
  role TEXT NOT NULL CHECK (role IN ('admin','viewer')),
  must_change_password BOOLEAN NOT NULL DEFAULT false,
  failed_logins INT NOT NULL DEFAULT 0 CHECK (failed_logins >= 0),
  locked_until TIMESTAMPTZ,
  disabled_at TIMESTAMPTZ,
  password_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_login_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_username_lower_key ON users (lower(username));

CREATE TABLE sessions (
  id_hash BYTEA PRIMARY KEY CHECK (octet_length(id_hash) = 32),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ NOT NULL,
  ip INET,
  user_agent TEXT CHECK (char_length(user_agent) <= 200),
  CHECK (expires_at > created_at)
);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- Sin FK a users: la auditoria sobrevive al borrado de usuarios (y no se hace UPDATE).
CREATE TABLE audit_log (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  ts TIMESTAMPTZ NOT NULL DEFAULT now(),
  actor_user_id UUID,
  actor_username TEXT CHECK (char_length(actor_username) <= 128),
  event TEXT NOT NULL CHECK (char_length(event) <= 64),
  outcome TEXT NOT NULL CHECK (outcome IN ('ok','fail','denied')),
  target_type TEXT CHECK (char_length(target_type) <= 64),
  target_id TEXT CHECK (char_length(target_id) <= 128),
  ip INET,
  detail JSONB CHECK (pg_column_size(detail) <= 8192)
);
CREATE INDEX audit_log_ts_idx ON audit_log (ts);
CREATE INDEX audit_log_actor_ts_idx ON audit_log (actor_user_id, ts DESC);

-- Append-only pragmatico: sin UPDATE; DELETE solo de filas > 30 dias (purga).
-- +goose StatementBegin
CREATE FUNCTION audit_log_guard() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  IF TG_OP = 'DELETE' AND OLD.ts < now() - interval '30 days' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'audit_log es append-only (solo se purgan filas de mas de 30 dias)'
    USING ERRCODE = 'insufficient_privilege';
END $$;
-- +goose StatementEnd
CREATE TRIGGER audit_log_guard BEFORE UPDATE OR DELETE ON audit_log
  FOR EACH ROW EXECUTE FUNCTION audit_log_guard();

-- enroll_tokens: token en claro -> token_hash (sha256 hex, igual que agent_token_hash).
ALTER TABLE enroll_tokens
  ADD COLUMN token_hash TEXT,
  ADD COLUMN created_by UUID REFERENCES users(id) ON DELETE SET NULL;
UPDATE enroll_tokens SET token_hash = encode(sha256(convert_to(token, 'UTF8')), 'hex');
ALTER TABLE enroll_tokens ALTER COLUMN token_hash SET NOT NULL;
-- +goose StatementBegin
DO $$
DECLARE pk text;
BEGIN
  SELECT conname INTO pk FROM pg_constraint
   WHERE conrelid = 'public.enroll_tokens'::regclass AND contype = 'p';
  EXECUTE format('ALTER TABLE public.enroll_tokens DROP CONSTRAINT %I', pk);
END $$;
-- +goose StatementEnd
ALTER TABLE enroll_tokens DROP COLUMN token;
ALTER TABLE enroll_tokens
  ADD CONSTRAINT enroll_tokens_pkey PRIMARY KEY (token_hash),
  ADD CONSTRAINT enroll_tokens_hash_fmt CHECK (token_hash ~ '^[0-9a-f]{64}$');
CREATE INDEX enroll_tokens_expires_at_idx ON enroll_tokens (expires_at);

ALTER TABLE hosts
  ADD COLUMN agent_token_revoked_at TIMESTAMPTZ,
  ADD CONSTRAINT hosts_revoked_disabled_check
    CHECK (agent_token_revoked_at IS NULL OR status = 'disabled');

-- +goose Down
-- Irreversible en datos: los enroll tokens en claro no se pueden reconstruir.
-- Se borran los no usados (no recuperables); los usados quedan con token = hash (solo historial).
ALTER TABLE hosts DROP CONSTRAINT hosts_revoked_disabled_check;
ALTER TABLE hosts DROP COLUMN agent_token_revoked_at;

DROP INDEX enroll_tokens_expires_at_idx;
DELETE FROM enroll_tokens WHERE used_at IS NULL;
ALTER TABLE enroll_tokens ADD COLUMN token TEXT;
UPDATE enroll_tokens SET token = token_hash;
ALTER TABLE enroll_tokens DROP CONSTRAINT enroll_tokens_pkey;
ALTER TABLE enroll_tokens DROP CONSTRAINT enroll_tokens_hash_fmt;
ALTER TABLE enroll_tokens ALTER COLUMN token SET NOT NULL;
ALTER TABLE enroll_tokens ADD CONSTRAINT enroll_tokens_pkey PRIMARY KEY (token);
ALTER TABLE enroll_tokens DROP COLUMN token_hash, DROP COLUMN created_by;

DROP TRIGGER audit_log_guard ON audit_log;
DROP FUNCTION audit_log_guard();
DROP TABLE audit_log;
DROP TABLE sessions;
DROP TABLE users;
