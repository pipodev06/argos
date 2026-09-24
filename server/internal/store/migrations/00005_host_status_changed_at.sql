-- +goose Up
-- Momento del ultimo cambio real de status (no se toca en cada ingest). Lo escriben el
-- ingest (down/pending -> up) y el detector de hosts caidos (up -> down); el enroll lo
-- fija con el DEFAULT. Filas existentes: la mejor aproximacion es el ultimo reporte.
ALTER TABLE hosts ADD COLUMN status_changed_at TIMESTAMPTZ NOT NULL DEFAULT now();
UPDATE hosts SET status_changed_at = COALESCE(last_seen_at, created_at);

-- +goose Down
ALTER TABLE hosts DROP COLUMN status_changed_at;
