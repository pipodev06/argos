package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
)

// Perfiles SNMP: credenciales reutilizables que se asignan a dispositivos. El secreto (un blob JSON con las
// contrasenas v3 o la comunidad v2c) va cifrado y la API nunca lo devuelve.
var (
	// ErrProfileExists: ya hay un perfil con ese nombre (sin distinguir mayusculas).
	ErrProfileExists = errors.New("ya existe un perfil snmp con ese nombre")
	// ErrTooManyProfiles: ya hay MaxSNMPProfiles perfiles.
	ErrTooManyProfiles = errors.New("demasiados perfiles snmp")
	// ErrProfileNotFound: el perfil referenciado (al asignarlo a un dispositivo) no existe.
	ErrProfileNotFound = errors.New("perfil snmp no encontrado")
	// ErrProfileInUse: hay dispositivos que usan el perfil.
	ErrProfileInUse = errors.New("el perfil snmp esta asignado a dispositivos")
)

// MaxSNMPProfiles es el maximo de perfiles.
const MaxSNMPProfiles = 100

const (
	profileNameKey = "snmp_profiles_name_lower_key"
	hostProfileFK  = "hosts_snmp_profile_id_fkey"
)

// Versiones SNMP admitidas (v1 no existe).
const (
	SNMPv2c = 2
	SNMPv3  = 3
)

// SNMPProfile es un perfil. En v3 Username, AuthProto y PrivProto existen y el nivel es siempre authPriv; en v2c
// (solo con confirmacion explicita del admin) no existen. SecretEnc guarda las contrasenas o la comunidad.
type SNMPProfile struct {
	ID          string
	Name        string
	Version     int
	Username    *string
	AuthProto   *string
	PrivProto   *string
	SecretEnc   []byte
	SecretKeyID string
	// DeviceCount son los dispositivos que lo usan (solo lectura).
	DeviceCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const snmpProfileSelect = `
SELECT p.id::text, p.name, p.version, p.username, p.auth_proto, p.priv_proto, p.secret_enc, p.secret_key_id,
       (SELECT count(*)::int FROM hosts h WHERE h.snmp_profile_id = p.id), p.created_at, p.updated_at
  FROM snmp_profiles p`

func scanSNMPProfile(row pgx.Row) (SNMPProfile, error) {
	var p SNMPProfile
	var ver int16
	err := row.Scan(&p.ID, &p.Name, &ver, &p.Username, &p.AuthProto, &p.PrivProto, &p.SecretEnc, &p.SecretKeyID,
		&p.DeviceCount, &p.CreatedAt, &p.UpdatedAt)
	p.Version = int(ver)
	return p, err
}

func mapProfileErr(err error, what string) error {
	if isUniqueViolation(err, profileNameKey) {
		return ErrProfileExists
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ListSNMPProfiles devuelve los perfiles por nombre (tope MaxSNMPProfiles).
func (s *Store) ListSNMPProfiles(ctx context.Context) ([]SNMPProfile, error) {
	rows, err := s.pool.Query(ctx, snmpProfileSelect+` ORDER BY lower(p.name), p.id LIMIT $1`, MaxSNMPProfiles)
	if err != nil {
		return nil, fmt.Errorf("listando perfiles snmp: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SNMPProfile, error) { return scanSNMPProfile(row) })
	if err != nil {
		return nil, fmt.Errorf("leyendo perfiles snmp: %w", err)
	}
	return out, nil
}

// SNMPProfileByID devuelve un perfil. ErrNotFound si no existe.
func (s *Store) SNMPProfileByID(ctx context.Context, id string) (SNMPProfile, error) {
	return snmpProfileByID(ctx, s.pool, id, "")
}

func snmpProfileByID(ctx context.Context, q rowQuerier, id, lock string) (SNMPProfile, error) {
	p, err := scanSNMPProfile(q.QueryRow(ctx, snmpProfileSelect+` WHERE p.id = $1 `+lock, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return SNMPProfile{}, ErrNotFound
	}
	if err != nil {
		return SNMPProfile{}, fmt.Errorf("leyendo perfil snmp: %w", err)
	}
	return p, nil
}

// CreateSNMPProfile inserta un perfil con el id ya generado (el secreto se cifro con ese id como AAD) y ya validado
// por quien llama. ErrTooManyProfiles o ErrProfileExists segun el caso.
func (s *Store) CreateSNMPProfile(ctx context.Context, p SNMPProfile, createdBy string) (SNMPProfile, error) {
	var out SNMPProfile
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('argos.snmp_profiles.cap'))`); err != nil {
			return fmt.Errorf("lock de perfiles snmp: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM snmp_profiles`).Scan(&n); err != nil {
			return fmt.Errorf("contando perfiles snmp: %w", err)
		}
		if n >= MaxSNMPProfiles {
			return ErrTooManyProfiles
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO snmp_profiles (id, name, version, username, auth_proto, priv_proto, secret_enc, secret_key_id, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, '')::uuid)`,
			p.ID, p.Name, p.Version, p.Username, p.AuthProto, p.PrivProto, p.SecretEnc, p.SecretKeyID, createdBy); err != nil {
			return mapProfileErr(err, "insertando perfil snmp")
		}
		var err error
		out, err = snmpProfileByID(ctx, tx, p.ID, "")
		return err
	})
	if err != nil {
		return SNMPProfile{}, err
	}
	return out, nil
}

// UpdateSNMPProfile bloquea el perfil, deja que mutate lo modifique (su error se devuelve tal cual) y guarda los
// campos editables (no la version). ErrNotFound o ErrProfileExists segun el caso.
func (s *Store) UpdateSNMPProfile(ctx context.Context, id string, mutate func(*SNMPProfile) error) (SNMPProfile, error) {
	var out SNMPProfile
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		p, err := snmpProfileByID(ctx, tx, id, "FOR UPDATE OF p")
		if err != nil {
			return err
		}
		if err := mutate(&p); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE snmp_profiles SET name = $2, username = $3, auth_proto = $4, priv_proto = $5, secret_enc = $6,
			        secret_key_id = $7, updated_at = now()
			  WHERE id = $1`,
			id, p.Name, p.Username, p.AuthProto, p.PrivProto, p.SecretEnc, p.SecretKeyID); err != nil {
			return mapProfileErr(err, "actualizando perfil snmp")
		}
		out, err = snmpProfileByID(ctx, tx, id, "")
		return err
	})
	if err != nil {
		return SNMPProfile{}, err
	}
	return out, nil
}

// DeleteSNMPProfile borra el perfil y devuelve su nombre. ErrNotFound si no existe; ErrProfileInUse si algun
// dispositivo lo usa.
func (s *Store) DeleteSNMPProfile(ctx context.Context, id string) (string, error) {
	var name string
	err := s.pool.QueryRow(ctx, `DELETE FROM snmp_profiles WHERE id = $1 RETURNING name`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if isFKViolation(err, hostProfileFK) {
		return "", ErrProfileInUse
	}
	if err != nil {
		return "", fmt.Errorf("borrando perfil snmp: %w", err)
	}
	return name, nil
}

// --- snmp: chequeos de dispositivos que entrega el colector ---

// SNMPAssignment es un chequeo snmp asignado a un dispositivo (con perfil) cuyo colector es un host concreto. ItemID
// es el id con que viaja al colector (uno por par chequeo-dispositivo, ver SNMPItemID).
type SNMPAssignment struct {
	Def      ServiceCheckDef
	DeviceID string
	Address  netip.Addr
	ItemID   string
	Profile  SNMPProfile
}

// CollectorSNMP devuelve los chequeos snmp habilitados de los dispositivos activos del colector que tienen perfil,
// por (chequeo, dispositivo), como maximo MaxChecksPerHost. Es lo que se entrega al colector (con el secreto del
// perfil todavia cifrado) y lo que valida sus resultados.
func (s *Store) CollectorSNMP(ctx context.Context, collectorID string) ([]SNMPAssignment, error) {
	return collectorSNMP(ctx, s.pool, collectorID)
}

func collectorSNMP(ctx context.Context, q queryer, collectorID string) ([]SNMPAssignment, error) {
	rows, err := q.Query(ctx,
		`SELECT `+checkDefCols+`, h.id::text, h.address, p.id::text, p.name, p.version, p.username, p.auth_proto,
		        p.priv_proto, p.secret_enc, p.secret_key_id
		   `+checkDefFrom+`
		   JOIN hosts h ON h.kind = 'device' AND h.collector_host_id = $1 AND h.status <> 'disabled'
		     AND h.group_id IS NOT DISTINCT FROM (SELECT c.group_id FROM hosts c WHERE c.id = $1)
		   JOIN snmp_profiles p ON p.id = h.snmp_profile_id
		  WHERE d.enabled AND d.type = 'snmp' AND `+hostInScopeSQL+` ORDER BY d.id, h.id LIMIT $2`,
		collectorID, MaxChecksPerHost)
	if err != nil {
		return nil, fmt.Errorf("resolviendo snmp del colector: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SNMPAssignment, error) {
		var a SNMPAssignment
		var ver int16
		p := &a.Profile
		d, err := scanCheckDef(row, &a.DeviceID, &a.Address, &p.ID, &p.Name, &ver, &p.Username, &p.AuthProto, &p.PrivProto,
			&p.SecretEnc, &p.SecretKeyID)
		a.Def, p.Version, a.ItemID = d, int(ver), SNMPItemID(d.ID, a.DeviceID)
		return a, err
	})
	if err != nil {
		return nil, fmt.Errorf("leyendo snmp del colector: %w", err)
	}
	return out, nil
}
