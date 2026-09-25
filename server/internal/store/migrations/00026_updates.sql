-- +goose Up
-- Parches del SO: permiso propio can_updates, ultima foto de parches por host, historial acotado y tabla de fin de
-- vida (EOL) del SO cargada a mano (sin salida a internet). Se usa una tabla propia y no inventory_current porque
-- esa exige items de tipo arreglo y un modulo de su lista; aqui la foto es un objeto con columnas tipadas
-- (cada una NULL = desconocido, nunca 0) y marcas derivadas en el servidor.

-- Permiso de lectura de parches (mismo patron que can_inventory: solo viewers; un admin lo ve todo).
ALTER TABLE users
  ADD COLUMN can_updates BOOLEAN NOT NULL DEFAULT false,
  DROP CONSTRAINT users_scope_viewer_only,
  ADD CONSTRAINT users_scope_viewer_only CHECK (role = 'viewer' OR (all_groups AND NOT can_inventory AND NOT can_updates));

CREATE TABLE updates_current (
  host_id UUID PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,
  rev TEXT NOT NULL CHECK (rev ~ '^[0-9a-f]{64}$'),
  pending_total INT CHECK (pending_total BETWEEN 0 AND 1000000),
  pending_security INT CHECK (pending_security BETWEEN 0 AND 1000000),
  reboot_required BOOLEAN,
  -- Marcas derivadas en el servidor (hora de recepcion). reboot_since: primera vez que se vio reinicio pendiente;
  -- security_pending_since: primera vez con pendientes de seguridad. Se limpian al dejar de cumplirse o pasar a
  -- desconocido (un dato desconocido no cuenta como "sigue pendiente").
  reboot_since TIMESTAMPTZ,
  security_pending_since TIMESTAMPTZ,
  last_installed DATE,
  source TEXT NOT NULL CHECK (source IN ('wua','apt','dnf','yum','zypper','softwareupdate','none')),
  cache_age_s BIGINT CHECK (cache_age_s BETWEEN 0 AND 315360000),
  advisories TEXT[] NOT NULL DEFAULT '{}' CHECK (cardinality(advisories) <= 200),
  advisories_truncated BOOLEAN NOT NULL DEFAULT false,
  collected_at TIMESTAMPTZ NOT NULL,
  received_at TIMESTAMPTZ NOT NULL,
  CHECK (pending_security IS NULL OR pending_total IS NULL OR pending_security <= pending_total),
  CHECK (reboot_since IS NULL OR reboot_required IS TRUE),
  CHECK (security_pending_since IS NULL OR pending_security > 0)
);

-- Historial: una fila por cambio de contadores o de reinicio (y la linea base). Tope diario por host en la app.
CREATE TABLE updates_history (
  id BIGSERIAL PRIMARY KEY,
  host_id UUID NOT NULL REFERENCES hosts(id) ON DELETE CASCADE,
  at TIMESTAMPTZ NOT NULL,
  pending_total INT,
  pending_security INT,
  reboot_required BOOLEAN
);
CREATE INDEX updates_history_host_at_idx ON updates_history (host_id, at DESC, id DESC);
CREATE INDEX updates_history_at_idx ON updates_history (at);

-- Fin de vida del SO. family/cycle es la clave que calcula os_eol_key(). eol_date = fin del soporte de seguridad
-- gratuito del proveedor (no ESM de pago). source: 'official' = fecha publicada por el proveedor y ya conocida;
-- 'estimate' = estimada (proveedor sin fecha publicada o version futura): revisar al actualizar la tabla.
CREATE TABLE os_eol (
  family TEXT NOT NULL,
  cycle TEXT NOT NULL,
  label TEXT NOT NULL,
  eol_date DATE NOT NULL,
  lts BOOLEAN NOT NULL DEFAULT false,
  source TEXT NOT NULL CHECK (source IN ('official','estimate')),
  PRIMARY KEY (family, cycle)
);

-- Seed (sin verificacion en linea: cargado de memoria del calendario de cada proveedor a 2026-09; por eso las
-- fechas futuras o no confirmadas van como 'estimate').
-- Windows 10 (Home/Pro; cycle = build): oficiales.
INSERT INTO os_eol (family, cycle, label, eol_date, lts, source) VALUES
  ('windows-10', '18363', 'Windows 10 1909', '2021-05-11', false, 'official'),
  ('windows-10', '19041', 'Windows 10 2004', '2021-12-14', false, 'official'),
  ('windows-10', '19042', 'Windows 10 20H2', '2022-05-10', false, 'official'),
  ('windows-10', '19043', 'Windows 10 21H1', '2022-12-13', false, 'official'),
  ('windows-10', '19044', 'Windows 10 21H2', '2023-06-13', false, 'official'),
  ('windows-10', '19045', 'Windows 10 22H2', '2025-10-14', false, 'official'),
-- Windows 11 (Home/Pro; cycle = build). 25H2 estimada (24 meses desde su lanzamiento).
  ('windows-11', '22000', 'Windows 11 21H2', '2023-10-10', false, 'official'),
  ('windows-11', '22621', 'Windows 11 22H2', '2024-10-08', false, 'official'),
  ('windows-11', '22631', 'Windows 11 23H2', '2025-11-11', false, 'official'),
  ('windows-11', '26100', 'Windows 11 24H2', '2026-10-13', false, 'official'),
  ('windows-11', '26200', 'Windows 11 25H2', '2027-10-12', false, 'estimate'),
-- Windows Server (fin del soporte extendido).
  ('windows-server', '2008-r2', 'Windows Server 2008 R2', '2020-01-14', false, 'official'),
  ('windows-server', '2012', 'Windows Server 2012', '2023-10-10', false, 'official'),
  ('windows-server', '2012-r2', 'Windows Server 2012 R2', '2023-10-10', false, 'official'),
  ('windows-server', '2016', 'Windows Server 2016', '2027-01-12', true, 'official'),
  ('windows-server', '2019', 'Windows Server 2019', '2029-01-09', true, 'official'),
  ('windows-server', '2022', 'Windows Server 2022', '2031-10-14', true, 'official'),
  ('windows-server', '2025', 'Windows Server 2025', '2034-10-10', true, 'official'),
-- Ubuntu: soporte estandar (5 anios LTS; 9 meses las demas). Las intermedias recientes y la 26.04 son estimadas.
  ('ubuntu', '16.04', 'Ubuntu 16.04 LTS', '2021-04-30', true, 'official'),
  ('ubuntu', '18.04', 'Ubuntu 18.04 LTS', '2023-05-31', true, 'official'),
  ('ubuntu', '20.04', 'Ubuntu 20.04 LTS', '2025-05-31', true, 'official'),
  ('ubuntu', '22.04', 'Ubuntu 22.04 LTS', '2027-04-30', true, 'official'),
  ('ubuntu', '24.04', 'Ubuntu 24.04 LTS', '2029-05-31', true, 'official'),
  ('ubuntu', '26.04', 'Ubuntu 26.04 LTS', '2031-05-31', true, 'estimate'),
  ('ubuntu', '22.10', 'Ubuntu 22.10', '2023-07-20', false, 'official'),
  ('ubuntu', '23.04', 'Ubuntu 23.04', '2024-01-25', false, 'official'),
  ('ubuntu', '23.10', 'Ubuntu 23.10', '2024-07-11', false, 'official'),
  ('ubuntu', '24.10', 'Ubuntu 24.10', '2025-07-10', false, 'official'),
  ('ubuntu', '25.04', 'Ubuntu 25.04', '2026-01-15', false, 'official'),
  ('ubuntu', '25.10', 'Ubuntu 25.10', '2026-07-09', false, 'estimate'),
-- Debian: fin del LTS comunitario (gratuito). 12 y 13 estimadas (fechas planificadas por el proyecto).
  ('debian', '9', 'Debian 9', '2022-06-30', false, 'official'),
  ('debian', '10', 'Debian 10', '2024-06-30', false, 'official'),
  ('debian', '11', 'Debian 11', '2026-08-31', false, 'official'),
  ('debian', '12', 'Debian 12', '2028-06-30', false, 'estimate'),
  ('debian', '13', 'Debian 13', '2030-06-30', false, 'estimate'),
-- RHEL y derivadas: fin del mantenimiento. RHEL 10 y derivadas 10 estimadas.
  ('rhel', '7', 'RHEL 7', '2024-06-30', false, 'official'),
  ('rhel', '8', 'RHEL 8', '2029-05-31', false, 'official'),
  ('rhel', '9', 'RHEL 9', '2032-05-31', false, 'official'),
  ('rhel', '10', 'RHEL 10', '2035-05-31', false, 'estimate'),
  ('centos', '7', 'CentOS 7', '2024-06-30', false, 'official'),
  ('rocky', '8', 'Rocky Linux 8', '2029-05-31', false, 'official'),
  ('rocky', '9', 'Rocky Linux 9', '2032-05-31', false, 'official'),
  ('rocky', '10', 'Rocky Linux 10', '2035-05-31', false, 'estimate'),
  ('almalinux', '8', 'AlmaLinux 8', '2029-03-01', false, 'official'),
  ('almalinux', '9', 'AlmaLinux 9', '2032-05-31', false, 'official'),
  ('almalinux', '10', 'AlmaLinux 10', '2035-05-31', false, 'estimate'),
-- macOS: Apple no publica fechas; se asume que cada version deja de recibir parches al salir la nueva (septiembre).
  ('macos', '11', 'macOS 11 Big Sur', '2023-09-26', false, 'estimate'),
  ('macos', '12', 'macOS 12 Monterey', '2024-09-16', false, 'estimate'),
  ('macos', '13', 'macOS 13 Ventura', '2025-09-15', false, 'estimate'),
  ('macos', '14', 'macOS 14 Sonoma', '2026-09-14', false, 'estimate'),
  ('macos', '15', 'macOS 15 Sequoia', '2027-09-13', false, 'estimate'),
  ('macos', '26', 'macOS 26 Tahoe', '2028-09-11', false, 'estimate');

-- os_eol_key resuelve (family, cycle) de un host a partir de hosts.os y host_info.os_name/os_version. Una sola
-- implementacion en SQL, reutilizable por el listado de flota y por las alertas. Sin coincidencia: 0 filas.
-- Windows: cliente por build de os_version ("10.0.<build>..."; build >= 22000 = 11); Server por el anio del nombre.
-- Linux: os_name es el id de la distro en minuscula (ubuntu, debian, rhel/redhat, centos, rocky, almalinux).
-- +goose StatementBegin
CREATE FUNCTION os_eol_key(p_os TEXT, p_name TEXT, p_version TEXT)
RETURNS TABLE (family TEXT, cycle TEXT) LANGUAGE sql IMMUTABLE AS $$
  WITH n AS (
    SELECT lower(coalesce(p_os, '')) AS os, lower(btrim(coalesce(p_name, ''))) AS name, btrim(coalesce(p_version, '')) AS v
  ), k AS (
    SELECT 'windows-server'::text AS f,
           replace((regexp_match(name, 'windows server (2008 r2|2012 r2|2012|2016|2019|2022|2025)'))[1], ' ', '-') AS c
      FROM n WHERE name LIKE '%windows server%'
    UNION ALL
    SELECT CASE WHEN b >= 22000 THEN 'windows-11' ELSE 'windows-10' END, b::text
      FROM (SELECT (regexp_match(v, '^10\.0\.([0-9]+)'))[1]::int AS b FROM n
             WHERE (os = 'windows' OR name LIKE '%windows%') AND name NOT LIKE '%server%') w
     WHERE b IS NOT NULL
    UNION ALL
    SELECT 'ubuntu', (regexp_match(v, '^([0-9]+\.[0-9]+)'))[1] FROM n WHERE name = 'ubuntu'
    UNION ALL
    SELECT 'debian', (regexp_match(v, '^([0-9]+)'))[1] FROM n WHERE name = 'debian'
    UNION ALL
    SELECT 'rhel', (regexp_match(v, '^([0-9]+)'))[1] FROM n WHERE name IN ('rhel', 'redhat') OR name LIKE 'red hat%'
    UNION ALL
    SELECT 'centos', (regexp_match(v, '^([0-9]+)'))[1] FROM n WHERE name = 'centos'
    UNION ALL
    SELECT 'rocky', (regexp_match(v, '^([0-9]+)'))[1] FROM n WHERE name = 'rocky'
    UNION ALL
    SELECT 'almalinux', (regexp_match(v, '^([0-9]+)'))[1] FROM n WHERE name IN ('almalinux', 'alma')
    UNION ALL
    SELECT 'macos', (regexp_match(v, '^([0-9]+)'))[1] FROM n WHERE name = 'macos' OR os = 'darwin'
  )
  SELECT f, c FROM k WHERE c IS NOT NULL LIMIT 1
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION os_eol_key(TEXT, TEXT, TEXT);
DROP TABLE os_eol;
DROP TABLE updates_history;
DROP TABLE updates_current;
ALTER TABLE users
  DROP CONSTRAINT users_scope_viewer_only,
  ADD CONSTRAINT users_scope_viewer_only CHECK (role = 'viewer' OR (all_groups AND NOT can_inventory)),
  DROP COLUMN can_updates;
