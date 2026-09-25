-- +goose Up
-- Nivel de privilegio con el que corre el agente (lo reporta el propio agente en host_info): 'user' (sin
-- privilegios, por defecto) o 'elevated' (instalado con --privileged). NULL = agente que aun no lo reporta.
ALTER TABLE host_info ADD COLUMN privilege TEXT CHECK (privilege IN ('user', 'elevated'));

-- +goose Down
ALTER TABLE host_info DROP COLUMN privilege;
