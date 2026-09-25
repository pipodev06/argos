-- +goose Up
-- Discos fisicos del host y los volumenes (mounts) que contiene cada uno (lo reporta el agente en host_info).
-- NULL = el agente no lo sabe (distinto de un arreglo vacio). Tope de 8 KiB en su forma de texto.
ALTER TABLE host_info ADD COLUMN physical_disks JSONB
    CHECK (jsonb_typeof(physical_disks) = 'array' AND octet_length(physical_disks::text) <= 8192);

-- +goose Down
ALTER TABLE host_info DROP COLUMN physical_disks;
