package checks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// gosnmpClient es el wrapper de gosnmp: solo GET de un OID, puerto fijo, v3 o v2c
// exactamente como se pidio (jamas degrada de una a otra) y sin logger (no filtra secretos).
// El buffer de lectura de gosnmp es de 65535 bytes: una respuesta que lo llena se descarta.
type gosnmpClient struct {
	port uint16 // 161; los tests usan un simulador local
}

func newGosnmpClient() *gosnmpClient { return &gosnmpClient{port: snmpPort} }

func (c *gosnmpClient) Get(ctx context.Context, req snmpRequest) (int64, error) {
	g, err := c.newSession(ctx, req)
	if err != nil {
		return 0, err
	}
	if err := g.Connect(); err != nil {
		return 0, mapSNMPError(ctx, err)
	}
	defer g.Conn.Close() //nolint:errcheck // cierre de un socket UDP: no hay nada que recuperar
	pkt, err := g.Get([]string{req.OID})
	if err != nil {
		return 0, mapSNMPError(ctx, err)
	}
	return pduValue(pkt, req.OID)
}

func (c *gosnmpClient) newSession(ctx context.Context, req snmpRequest) (*gosnmp.GoSNMP, error) {
	p := req.Params
	g := &gosnmp.GoSNMP{
		Target:    req.IP.String(),
		Port:      c.port,
		Transport: "udp",
		Timeout:   min(req.Timeout, snmpMaxTimeout),
		Retries:   snmpRetries,
		MaxOids:   1,
		Context:   ctx,
	}
	switch p.Version {
	case 3:
		auth, okA := map[string]gosnmp.SnmpV3AuthProtocol{"SHA256": gosnmp.SHA256, "SHA512": gosnmp.SHA512}[p.AuthProto]
		priv, okP := map[string]gosnmp.SnmpV3PrivProtocol{"AES128": gosnmp.AES, "AES256": gosnmp.AES256}[p.PrivProto]
		if !okA || !okP || p.Level != "authPriv" {
			return nil, errors.New("parametros snmp v3 no permitidos")
		}
		g.Version = gosnmp.Version3
		g.SecurityModel = gosnmp.UserSecurityModel
		g.MsgFlags = gosnmp.AuthPriv
		g.SecurityParameters = &gosnmp.UsmSecurityParameters{
			UserName:                 p.User,
			AuthenticationProtocol:   auth,
			AuthenticationPassphrase: p.AuthPass,
			PrivacyProtocol:          priv,
			PrivacyPassphrase:        p.PrivPass,
		}
	case 2:
		g.Version = gosnmp.Version2c
		g.Community = p.Community
	default:
		return nil, errors.New("version snmp no permitida")
	}
	return g, nil
}

// pduValue extrae el entero de la unica variable de la respuesta.
func pduValue(pkt *gosnmp.SnmpPacket, oid string) (int64, error) {
	if pkt == nil || len(pkt.Variables) != 1 {
		return 0, errors.New("respuesta snmp inesperada")
	}
	if pkt.Error != gosnmp.NoError {
		if pkt.Error == gosnmp.NoSuchName {
			return 0, ErrSNMPNoSuchObject
		}
		return 0, fmt.Errorf("snmp error-status %d", pkt.Error)
	}
	v := pkt.Variables[0]
	if v.Name != "."+oid {
		return 0, errors.New("respuesta snmp de otro oid")
	}
	switch v.Type {
	case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView:
		return 0, ErrSNMPNoSuchObject
	}
	switch n := v.Value.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case uint:
		return int64(min(uint64(n), 1<<62)), nil
	case uint32:
		return int64(n), nil
	case uint64:
		return int64(min(n, 1<<62)), nil
	default:
		return 0, errors.New("valor snmp no entero")
	}
}

// mapSNMPError traduce errores de gosnmp (que no siempre traen sentinela) a los del paquete.
func mapSNMPError(ctx context.Context, err error) error {
	var ne net.Error
	msg := err.Error()
	switch {
	case errors.Is(err, gosnmp.ErrUnknownUsername), errors.Is(err, gosnmp.ErrWrongDigest),
		errors.Is(err, gosnmp.ErrDecryption), errors.Is(err, gosnmp.ErrUnknownSecurityLevel):
		return ErrSNMPAuth
	case strings.Contains(msg, "not authentic"): // gosnmp: el digest HMAC de la respuesta no coincide (clave de autenticacion distinta)
		return ErrSNMPAuth
	case strings.Contains(msg, "response buffer too small"): // gosnmp: datagrama >= 65535 bytes
		return ErrSNMPTooLarge
	case errors.Is(err, context.DeadlineExceeded), ctx.Err() != nil,
		errors.As(err, &ne) && ne.Timeout(),
		strings.Contains(msg, "request timeout"),    // gosnmp no expone sentinela
		strings.Contains(msg, "connection refused"): // ICMP port unreachable
		return ErrSNMPTimeout
	default:
		return err
	}
}
