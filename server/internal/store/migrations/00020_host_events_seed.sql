-- +goose Up
-- host_events (00014) pasa a ser la base del SLA: un host debe tener siempre un evento inicial. from_status
-- puede ser NULL (evento inicial: alta por enroll o siembra de esta migracion) y source admite 'enroll' y 'seed'.
-- Las transiciones siguen escribiendose en la misma sentencia que cambia hosts.status (un CTE por camino), no con trigger.
ALTER TABLE host_events ALTER COLUMN from_status DROP NOT NULL;
ALTER TABLE host_events DROP CONSTRAINT host_events_source_check;
ALTER TABLE host_events ADD CONSTRAINT host_events_source_check
  CHECK (source IN ('detector', 'ingest', 'admin', 'group_change', 'enroll', 'seed'));

-- Siembra: 1 evento por host que aun no tiene historial. No se inventa el pasado; el primer evento es el
-- inicio del seguimiento (tracked_from).
INSERT INTO host_events (host_id, from_status, to_status, at, source)
SELECT h.id, NULL, h.status, COALESCE(h.status_changed_at, h.created_at), 'seed'
  FROM hosts h
 WHERE NOT EXISTS (SELECT 1 FROM host_events e WHERE e.host_id = h.id);

-- +goose Down
DELETE FROM host_events WHERE from_status IS NULL OR source IN ('enroll', 'seed');
ALTER TABLE host_events DROP CONSTRAINT host_events_source_check;
ALTER TABLE host_events ADD CONSTRAINT host_events_source_check
  CHECK (source IN ('detector', 'ingest', 'admin', 'group_change'));
ALTER TABLE host_events ALTER COLUMN from_status SET NOT NULL;
