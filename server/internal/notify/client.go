package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"
)

// Categorias genericas de fallo de un envio: son lo unico que sale al cliente de la API o a last_error.
const (
	CatDNS     = "dns"
	CatConnect = "connect"
	CatTLS     = "tls"
	CatTimeout = "timeout"
	CatStatus  = "status"
)

const (
	sendTimeout    = 5 * time.Second
	maxDrainBytes  = 4 << 10
	dialRetryLimit = 4 // IPs que se prueban como maximo por conexion
)

// Target es a donde y como se envia: URL (puede llevar un token) y secreto HMAC opcional.
type Target struct {
	URL        string
	HMACSecret string
}

// SendError es un fallo de envio ya clasificado. No incluye URL, IP ni cuerpo de la respuesta.
type SendError struct {
	Category string
	// HTTPStatus es el codigo recibido (solo con Category == CatStatus).
	HTTPStatus int
	// Blocked marca que el destino fue rechazado por la politica (se reporta como "connect" al exterior).
	Blocked bool
}

func (e *SendError) Error() string {
	switch {
	case e.Blocked:
		return "destino bloqueado por la politica de red"
	case e.Category == CatStatus:
		return "respuesta http " + strconv.Itoa(e.HTTPStatus)
	default:
		return "fallo de envio: " + e.Category
	}
}

// Resolver resuelve nombres a IPs (net.Resolver en produccion; falso en tests).
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Client envia webhooks aplicando la politica anti-SSRF. Se crea una sola vez y se reutiliza.
type Client struct {
	policy *Policy
	res    Resolver
	http   *http.Client
	now    func() time.Time
}

// ClientOptions ajusta el cliente. El cero es produccion: resolvedor del sistema y raices TLS del sistema.
type ClientOptions struct {
	Resolver Resolver
	// RootCAs reemplaza las raices del sistema (nil = las del sistema). ARGOS_WEBHOOK_CA_FILE pasa un pool que
	// ya incluye las del sistema mas la CA propia.
	RootCAs *x509.CertPool
	Now     func() time.Time
}

// NewClient crea el cliente webhook: sin proxy, sin redirects, TLS verificado, timeout de 5 s, y dialer que
// valida la IP resuelta.
func NewClient(policy *Policy, opts ClientOptions) *Client {
	c := &Client{policy: policy, res: opts.Resolver, now: opts.Now}
	if c.res == nil {
		c.res = net.DefaultResolver
	}
	if c.now == nil {
		c.now = time.Now
	}
	tr := &http.Transport{
		Proxy:                  nil,
		DialContext:            c.dialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: opts.RootCAs},
		TLSHandshakeTimeout:    sendTimeout,
		ResponseHeaderTimeout:  sendTimeout,
		DisableKeepAlives:      true, // cada envio resuelve y valida de nuevo
		MaxResponseHeaderBytes: 16 << 10,
	}
	c.http = &http.Client{
		Transport: tr,
		Timeout:   sendTimeout,
		// Un redirect podria llevar a una IP interna: no se sigue; el 3xx cuenta como fallo de status.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return c
}

// lookup devuelve las IPs de host (una sola resolucion). Un literal no consulta DNS.
func (c *Client) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	ips, err := c.res.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, &dnsError{}
	}
	return ips, nil
}

type dnsError struct{}

func (*dnsError) Error() string { return "no se pudo resolver el destino" }

// dialContext resuelve una vez, valida TODAS las IPs y conecta a una de esas IPs (no vuelve a resolver).
func (c *Client) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, ErrBlockedDestination
	}
	hostOK := c.policy.hostAllowed(host)
	if port != defaultPort && !hostOK {
		return nil, ErrBlockedDestination
	}
	ips, err := c.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if err := c.policy.checkIP(ip, hostOK); err != nil {
			return nil, err
		}
	}
	d := &net.Dialer{Timeout: sendTimeout, Control: c.policy.control(hostOK)}
	var lastErr error
	for i, ip := range ips {
		if i >= dialRetryLimit {
			break
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// CheckURL valida una URL antes de guardarla: forma (ParseTarget) y que todas las IPs a las que resuelve
// esten permitidas. Un host que no resuelve es un error.
func (c *Client) CheckURL(ctx context.Context, raw string) error {
	u, err := c.policy.ParseTarget(raw)
	if err != nil {
		return err
	}
	host := u.Hostname()
	if _, err := netip.ParseAddr(host); err == nil {
		return nil // literal: ParseTarget ya lo valido
	}
	ips, err := c.lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: el host no resuelve", ErrURLInvalid)
	}
	hostOK := c.policy.hostAllowed(host)
	for _, ip := range ips {
		if err := c.policy.checkIP(ip, hostOK); err != nil {
			return err
		}
	}
	return nil
}

// Sign calcula la firma HMAC-SHA256 de "timestamp.cuerpo" en hex.
func Sign(secret string, ts int64, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(strconv.FormatInt(ts, 10)))
	m.Write([]byte("."))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

// Send hace POST de body (JSON) a t.URL y devuelve el codigo HTTP recibido (0 si no hubo respuesta). El error es
// nil solo con una respuesta 2xx; el resto es *SendError.
func (c *Client) Send(ctx context.Context, t Target, body []byte) (int, error) {
	if _, err := c.policy.ParseTarget(t.URL); err != nil {
		return 0, &SendError{Category: CatConnect, Blocked: true}
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.URL, bytes.NewReader(body))
	if err != nil {
		return 0, &SendError{Category: CatConnect, Blocked: true}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "argos-server")
	if t.HMACSecret != "" {
		ts := c.now().Unix()
		req.Header.Set("X-Argos-Timestamp", strconv.FormatInt(ts, 10))
		req.Header.Set("X-Argos-Signature", "sha256="+Sign(t.HMACSecret, ts, body))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, classify(err)
	}
	defer func() { _ = resp.Body.Close() }()
	// El cuerpo se descarta (acotado): nunca se lee ni se guarda.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return resp.StatusCode, &SendError{Category: CatStatus, HTTPStatus: resp.StatusCode}
	}
	return resp.StatusCode, nil
}

// classify traduce un error de transporte a una categoria generica.
func classify(err error) *SendError {
	var (
		dnsErr   *dnsError
		certErr  *tls.CertificateVerificationError
		recErr   tls.RecordHeaderError
		alertErr tls.AlertError
		unknown  x509.UnknownAuthorityError
		hostErr  x509.HostnameError
		invalid  x509.CertificateInvalidError
		netErr   net.Error
	)
	switch {
	case errors.Is(err, ErrBlockedDestination):
		return &SendError{Category: CatConnect, Blocked: true}
	case errors.As(err, &dnsErr):
		return &SendError{Category: CatDNS}
	case errors.As(err, &certErr), errors.As(err, &recErr), errors.As(err, &alertErr),
		errors.As(err, &unknown), errors.As(err, &hostErr), errors.As(err, &invalid):
		return &SendError{Category: CatTLS}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return &SendError{Category: CatTimeout}
	default:
		return &SendError{Category: CatConnect}
	}
}
