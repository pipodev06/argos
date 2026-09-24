-- +goose Up
-- Particionado nativo por rango de tiempo (dias UTC) de metrics y service_checks,
-- resumenes por hora (metrics_hourly) y estado del mantenimiento.
-- Conversion SIN copiar filas: la tabla vieja pasa a ser la particion "legacy"
-- [MINVALUE, inicio del dia UTC de pasado manana) y las particiones diarias nuevas
-- empiezan justo despues. Toda la migracion corre en una transaccion (goose por defecto).

-- ===================== metrics =====================
LOCK TABLE metrics IN ACCESS EXCLUSIVE MODE;
ALTER TABLE metrics RENAME TO metrics_legacy;
ALTER INDEX metrics_pkey RENAME TO metrics_legacy_pkey;   -- renombra tambien la constraint

CREATE TABLE metrics (
  host_id UUID NOT NULL REFERENCES hosts(id),
  ts TIMESTAMPTZ NOT NULL,
  cpu_pct REAL,
  mem_used_mb INT,
  mem_total_mb INT,
  net_rx_bytes BIGINT,
  net_tx_bytes BIGINT,
  PRIMARY KEY (host_id, ts)          -- incluye la clave de particion: ON CONFLICT (host_id, ts) sigue valido
) PARTITION BY RANGE (ts);

-- BRIN sobre ts: los inserts llegan casi en orden de tiempo; deja al resumen por hora leer solo los
-- bloques de su hora en vez de todo el dia (cuesta KB por particion).
CREATE INDEX metrics_ts_brin_idx ON metrics USING brin (ts) WITH (pages_per_range = 32);

-- ===================== service_checks =====================
-- Sin PK: una PK debe incluir la clave de particion, y (id, ts) con id UUID aleatorio es el peor indice pa
-- una tabla de solo-append. Nadie lee ni referencia service_checks.id. La tabla hoy no se consulta.
LOCK TABLE service_checks IN ACCESS EXCLUSIVE MODE;
ALTER TABLE service_checks RENAME TO service_checks_legacy;
ALTER TABLE service_checks_legacy DROP COLUMN id;          -- arrastra service_checks_pkey
ALTER INDEX service_checks_host_id_ts_idx RENAME TO service_checks_legacy_host_id_ts_idx;

CREATE TABLE service_checks (
  host_id UUID NOT NULL REFERENCES hosts(id),
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  response_ms INT,
  ts TIMESTAMPTZ NOT NULL,
  CONSTRAINT service_checks_status_check CHECK (status IN ('up', 'down', 'warn', 'unknown'))
) PARTITION BY RANGE (ts);
CREATE INDEX service_checks_host_id_ts_idx ON service_checks (host_id, ts);

-- ===================== adopcion de las tablas viejas + particiones diarias =====================
-- +goose StatementBegin
DO $mig$
DECLARE
  w timestamp := date_trunc('day', now() AT TIME ZONE 'UTC') + interval '2 days';  -- inicio (UTC, sin zona) del primer dia diario
  legacy_upper timestamptz := w AT TIME ZONE 'UTC';
  n int;
  lo timestamptz;
  hi timestamptz;
  suffix text;
BEGIN
  -- CHECK equivalente a la cota: ATTACH lo reconoce y se salta su propio escaneo (un solo escaneo, el del CHECK).
  EXECUTE format('ALTER TABLE metrics_legacy ADD CONSTRAINT metrics_legacy_bound CHECK (ts < %L)', legacy_upper);
  EXECUTE format('ALTER TABLE metrics ATTACH PARTITION metrics_legacy FOR VALUES FROM (MINVALUE) TO (%L)', legacy_upper);
  ALTER TABLE metrics_legacy DROP CONSTRAINT metrics_legacy_bound;

  EXECUTE format('ALTER TABLE service_checks_legacy ADD CONSTRAINT service_checks_legacy_bound CHECK (ts < %L)', legacy_upper);
  EXECUTE format('ALTER TABLE service_checks ATTACH PARTITION service_checks_legacy FOR VALUES FROM (MINVALUE) TO (%L)', legacy_upper);
  ALTER TABLE service_checks_legacy DROP CONSTRAINT service_checks_legacy_bound;

  -- 7 dias diarios adelante de la particion legacy. Nombre = dia UTC; cotas con offset explicito
  -- (independiente de la zona horaria de la sesion). El mantenimiento en Go usa el mismo esquema.
  FOR n IN 0..6 LOOP
    lo := (w + n * interval '1 day') AT TIME ZONE 'UTC';
    hi := (w + (n + 1) * interval '1 day') AT TIME ZONE 'UTC';
    suffix := to_char(w + n * interval '1 day', 'YYYYMMDD');
    EXECUTE format('CREATE TABLE %I PARTITION OF metrics FOR VALUES FROM (%L) TO (%L)', 'metrics_p' || suffix, lo, hi);
    EXECUTE format('CREATE TABLE %I PARTITION OF service_checks FOR VALUES FROM (%L) TO (%L)', 'service_checks_p' || suffix, lo, hi);
  END LOOP;
END
$mig$;
-- +goose StatementEnd

-- ===================== metrics_hourly =====================
-- Un resumen por (host, hora UTC cerrada). Solo lo que usan las graficas (30d/90d/1a) y las alertas por umbral.
-- Columnas de 4 bytes agrupadas tras uuid+timestamptz pa no perder bytes de alineacion.
CREATE TABLE metrics_hourly (
  host_id UUID NOT NULL REFERENCES hosts(id),
  hour TIMESTAMPTZ NOT NULL,          -- inicio de la hora, alineado a UTC
  samples INT NOT NULL,               -- filas crudas de esa hora (peso al reagrupar en buckets mayores)
  cpu_avg REAL, cpu_min REAL, cpu_max REAL,
  mem_used_avg_mb INT, mem_used_max_mb INT, mem_total_mb INT,
  net_rx_bps_avg REAL, net_rx_bps_max REAL,     -- bytes/s; NULL si toda la hora fue hueco/reinicio
  net_tx_bps_avg REAL, net_tx_bps_max REAL,
  PRIMARY KEY (host_id, hour)
);
-- Retencion (DELETE por hora < corte) sin recorrer toda la tabla: las horas se insertan en orden.
CREATE INDEX metrics_hourly_hour_brin_idx ON metrics_hourly USING brin (hour);

-- Marcas del mantenimiento. 'metrics_rollup_through' = instante hasta el cual todas las horas
-- cerradas estan resumidas; una particion cruda no se borra mientras su cota superior lo supere.
CREATE TABLE maintenance_state (
  name TEXT PRIMARY KEY,
  value_ts TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
-- Vuelve a tablas planas copiando las filas (Down es raro; el Up no copia). Los resumenes se pierden
-- (son derivados: se recalculan desde crudo solo mientras el crudo exista).
DROP TABLE maintenance_state;
DROP TABLE metrics_hourly;

CREATE TABLE metrics_flat (
  host_id UUID NOT NULL REFERENCES hosts(id),
  ts TIMESTAMPTZ NOT NULL,
  cpu_pct REAL,
  mem_used_mb INT,
  mem_total_mb INT,
  net_rx_bytes BIGINT,
  net_tx_bytes BIGINT,
  PRIMARY KEY (host_id, ts)
);
INSERT INTO metrics_flat SELECT host_id, ts, cpu_pct, mem_used_mb, mem_total_mb, net_rx_bytes, net_tx_bytes FROM metrics;
DROP TABLE metrics;            -- borra el padre y todas las particiones
ALTER TABLE metrics_flat RENAME TO metrics;
ALTER INDEX metrics_flat_pkey RENAME TO metrics_pkey;
ALTER TABLE metrics RENAME CONSTRAINT metrics_flat_host_id_fkey TO metrics_host_id_fkey;

CREATE TABLE service_checks_flat (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  host_id UUID NOT NULL REFERENCES hosts(id),
  name TEXT NOT NULL,
  status TEXT NOT NULL,
  response_ms INT,
  ts TIMESTAMPTZ NOT NULL,
  CONSTRAINT service_checks_status_check CHECK (status IN ('up', 'down', 'warn', 'unknown'))
);
INSERT INTO service_checks_flat (host_id, name, status, response_ms, ts)
  SELECT host_id, name, status, response_ms, ts FROM service_checks;
DROP TABLE service_checks;
ALTER TABLE service_checks_flat RENAME TO service_checks;
ALTER INDEX service_checks_flat_pkey RENAME TO service_checks_pkey;
ALTER TABLE service_checks RENAME CONSTRAINT service_checks_flat_host_id_fkey TO service_checks_host_id_fkey;
CREATE INDEX service_checks_host_id_ts_idx ON service_checks (host_id, ts);
