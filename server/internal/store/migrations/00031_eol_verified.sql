-- +goose Up
-- Fin de vida verificado en linea el 2026-09-25 (antes de esta migracion la semilla de 00026 estaba cargada de
-- memoria). Fuentes: Microsoft Learn (Windows 11 Home y Pro: https://learn.microsoft.com/lifecycle/products/
-- windows-11-home-and-pro; las fechas de la pagina van un dia despues de la publicada, p. ej. 25H2 = 13/10/2027
-- 06:59 UTC = 12/10/2027) y endoflife.date (Ubuntu, Debian, RHEL, Rocky, AlmaLinux, macOS), que recoge las paginas
-- de cada proveedor. Windows 10 y Windows Server NO se re-verificaron (siguen de la semilla original). macOS: Apple
-- no publica fechas, se dejan estimadas.
UPDATE os_eol SET eol_date = '2027-10-12', source = 'official' WHERE family = 'windows-11' AND cycle = '26200';

UPDATE os_eol SET eol_date = '2027-06-01' WHERE family = 'ubuntu' AND cycle = '22.04';
UPDATE os_eol SET eol_date = '2024-01-20' WHERE family = 'ubuntu' AND cycle = '23.04';
UPDATE os_eol SET eol_date = '2024-07-12' WHERE family = 'ubuntu' AND cycle = '23.10';
UPDATE os_eol SET eol_date = '2026-01-17' WHERE family = 'ubuntu' AND cycle = '25.04';
UPDATE os_eol SET eol_date = '2026-07-01', source = 'official' WHERE family = 'ubuntu' AND cycle = '25.10';
UPDATE os_eol SET eol_date = '2031-05-29', source = 'official' WHERE family = 'ubuntu' AND cycle = '26.04';

UPDATE os_eol SET eol_date = '2022-07-01' WHERE family = 'debian' AND cycle = '9';
UPDATE os_eol SET source = 'official' WHERE family = 'debian' AND cycle IN ('12', '13');

UPDATE os_eol SET source = 'official' WHERE family IN ('rhel', 'rocky', 'almalinux') AND cycle = '10';
UPDATE os_eol SET eol_date = '2029-05-31' WHERE family = 'almalinux' AND cycle = '8';

INSERT INTO os_eol (family, cycle, label, eol_date, lts, source) VALUES
  ('macos', '27', 'macOS 27 Golden Gate', '2029-09-13', false, 'estimate')
ON CONFLICT (family, cycle) DO NOTHING;

-- Windows cliente: el agente reporta os_name="Microsoft Windows 11 Pro", os_version="25H2" y deja el build ("10.0.
-- 26200...") en kernel_version, asi que la clave por build de 00026 nunca coincidia (fin de vida "Desconocido"). Se
-- anade la resolucion por el nombre de la actualizacion de caracteristicas cuando os_version no trae el build.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION os_eol_key(p_os TEXT, p_name TEXT, p_version TEXT)
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
    SELECT CASE WHEN name LIKE '%windows 11%' THEN 'windows-11' ELSE 'windows-10' END,
           CASE WHEN name LIKE '%windows 11%' THEN
                  CASE upper(v) WHEN '21H2' THEN '22000' WHEN '22H2' THEN '22621' WHEN '23H2' THEN '22631'
                                WHEN '24H2' THEN '26100' WHEN '25H2' THEN '26200' END
                ELSE
                  CASE upper(v) WHEN '1909' THEN '18363' WHEN '2004' THEN '19041' WHEN '20H2' THEN '19042'
                                WHEN '21H1' THEN '19043' WHEN '21H2' THEN '19044' WHEN '22H2' THEN '19045' END
           END
      FROM n
     WHERE (os = 'windows' OR name LIKE '%windows%') AND name NOT LIKE '%server%'
       AND (regexp_match(v, '^10\.0\.([0-9]+)')) IS NULL
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
-- Vuelve a la funcion de 00026 (solo clave por build) y a las fechas de la semilla original.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION os_eol_key(p_os TEXT, p_name TEXT, p_version TEXT)
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
DELETE FROM os_eol WHERE family = 'macos' AND cycle = '27';
UPDATE os_eol SET eol_date = '2027-10-12', source = 'estimate' WHERE family = 'windows-11' AND cycle = '26200';
UPDATE os_eol SET eol_date = '2027-04-30' WHERE family = 'ubuntu' AND cycle = '22.04';
UPDATE os_eol SET eol_date = '2024-01-25' WHERE family = 'ubuntu' AND cycle = '23.04';
UPDATE os_eol SET eol_date = '2024-07-11' WHERE family = 'ubuntu' AND cycle = '23.10';
UPDATE os_eol SET eol_date = '2026-01-15' WHERE family = 'ubuntu' AND cycle = '25.04';
UPDATE os_eol SET eol_date = '2026-07-09', source = 'estimate' WHERE family = 'ubuntu' AND cycle = '25.10';
UPDATE os_eol SET eol_date = '2031-05-31', source = 'estimate' WHERE family = 'ubuntu' AND cycle = '26.04';
UPDATE os_eol SET eol_date = '2022-06-30' WHERE family = 'debian' AND cycle = '9';
UPDATE os_eol SET source = 'estimate' WHERE family = 'debian' AND cycle IN ('12', '13');
UPDATE os_eol SET source = 'estimate' WHERE family IN ('rhel', 'rocky', 'almalinux') AND cycle = '10';
UPDATE os_eol SET eol_date = '2029-03-01' WHERE family = 'almalinux' AND cycle = '8';
