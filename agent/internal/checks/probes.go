package checks

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// allowedHeaders: unicas cabeceras que puede llevar el secreto de un chequeo http.
var allowedHeaders = map[string]bool{"Authorization": true, "X-Api-Key": true, "X-Auth-Token": true}

// probe hace UN intento del chequeo (sin reintentos).
func (r *Runner) probe(ctx context.Context, c Check) outcome {
	ctx, cancel := context.WithTimeout(ctx, timeoutOf(c))
	defer cancel()
	switch c.Type {
	case TypeTCP:
		return r.probeTCP(ctx, c)
	case TypeHTTP:
		return r.probeHTTP(ctx, c)
	case TypeTLSCert:
		return r.probeTLS(ctx, c)
	case TypeService:
		return r.probeService(ctx, c)
	default:
		return unknown(DetailUnsupported)
	}
}

func timeoutOf(c Check) time.Duration {
	ms := c.TimeoutMS
	if ms <= 0 {
		ms = defaultTimeout
	}
	return time.Duration(min(max(ms, minTimeoutMS), MaxTimeoutMS)) * time.Millisecond
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

func (r *Runner) probeTCP(ctx context.Context, c Check) outcome {
	if c.TargetHost == "" || !validPort(c.TargetPort) {
		return unknown(DetailUnsupported)
	}
	start := r.now()
	conn, err := r.dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.TargetHost, strconv.Itoa(c.TargetPort)))
	if err != nil {
		return classify(err)
	}
	ms := r.now().Sub(start).Milliseconds()
	_ = conn.Close() // solo se mide la conexion: no se envia nada
	return latencyOutcome(ms, c.WarnMS, c.CritMS)
}

func (r *Runner) probeHTTP(ctx context.Context, c Check) outcome {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return unknown(DetailUnsupported)
	}
	lo, hi, ok := parseExpect(c.ExpectStatus)
	if !ok {
		return unknown(DetailUnsupported)
	}
	// La request se arma desde las partes: esquema, host, puerto y ruta.
	target := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), http.NoBody)
	if err != nil {
		return unknown(DetailUnsupported)
	}
	if c.HeaderName != "" || c.Secret != "" {
		name := http.CanonicalHeaderKey(c.HeaderName)
		if !allowedHeaders[name] || c.Secret == "" {
			return unknown(DetailUnsupported)
		}
		req.Header.Set(name, c.Secret)
	}

	start := r.now()
	resp, err := r.client.Do(req)
	if err != nil {
		return classify(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < lo || resp.StatusCode > hi {
		return outcome{status: StatusDown, detail: DetailStatusMismatch, ms: r.now().Sub(start).Milliseconds()}
	}
	if c.ExpectText != "" {
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes))
		if rerr != nil && ctx.Err() != nil {
			return classify(ctx.Err())
		}
		if !bytes.Contains(body, []byte(c.ExpectText)) {
			return outcome{status: StatusDown, detail: DetailTextMismatch, ms: r.now().Sub(start).Milliseconds()}
		}
	}
	return latencyOutcome(r.now().Sub(start).Milliseconds(), c.WarnMS, c.CritMS)
}

// parseExpect entiende "200" o "200-399"; vacio = 200-399.
func parseExpect(s string) (lo, hi int, ok bool) {
	if s == "" {
		s = defaultExpect
	}
	a, b, isRange := strings.Cut(s, "-")
	lo, err := strconv.Atoi(a)
	if err != nil || lo < 100 || lo > 599 {
		return 0, 0, false
	}
	if !isRange {
		return lo, lo, true
	}
	hi, err = strconv.Atoi(b)
	if err != nil || hi < lo || hi > 599 {
		return 0, 0, false
	}
	return lo, hi, true
}

// probeTLS hace SOLO el handshake TLS y lee NotAfter: sin enviar datos. Es el unico
// sitio con InsecureSkipVerify (excepcion de seguridad aprobada); la validez de la
// cadena se calcula aparte y sale como `trusted`.
func (r *Runner) probeTLS(ctx context.Context, c Check) outcome {
	if c.TargetHost == "" || !validPort(c.TargetPort) {
		return unknown(DetailUnsupported)
	}
	start := r.now()
	raw, err := r.dialer.DialContext(ctx, "tcp", net.JoinHostPort(c.TargetHost, strconv.Itoa(c.TargetPort)))
	if err != nil {
		return classify(err)
	}
	// Se cierra el socket directo (no tls.Conn.Close): no se manda ni el alert de cierre.
	defer func() { _ = raw.Close() }()
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, //nolint:gosec // solo lee NotAfter; la cadena se verifica abajo (trusted)
	}
	if net.ParseIP(c.TargetHost) == nil {
		cfg.ServerName = c.TargetHost
	}
	tc := tls.Client(raw, cfg)
	if err := tc.HandshakeContext(ctx); err != nil {
		if ctx.Err() != nil {
			return classify(ctx.Err())
		}
		return outcome{status: StatusDown, detail: DetailTLS}
	}
	ms := r.now().Sub(start).Milliseconds()
	peers := tc.ConnectionState().PeerCertificates
	if len(peers) == 0 {
		return outcome{status: StatusDown, detail: DetailTLS, ms: ms}
	}
	now := r.now()
	leaf := peers[0]
	days := math.Round(leaf.NotAfter.Sub(now).Hours()/24*10) / 10
	opts := x509.VerifyOptions{Roots: r.roots, Intermediates: x509.NewCertPool(), CurrentTime: now}
	for _, ic := range peers[1:] {
		opts.Intermediates.AddCert(ic)
	}
	if cfg.ServerName != "" {
		opts.DNSName = cfg.ServerName
	}
	_, verr := leaf.Verify(opts)
	trusted := verr == nil
	out := outcome{ms: ms, value: &days, trusted: &trusted}
	switch {
	case days < 0:
		out.status, out.detail = StatusDown, DetailExpired
	case days <= c.CritDays:
		out.status, out.detail = StatusDown, DetailOK
	case days <= c.WarnDays:
		out.status, out.detail = StatusWarn, DetailOK
	default:
		out.status, out.detail = StatusUp, DetailOK
	}
	return out
}

func (r *Runner) probeService(ctx context.Context, c Check) outcome {
	if !validServiceName(c.ServiceName) {
		return unknown(DetailUnsupported)
	}
	up, err := r.service.Probe(ctx, c.ServiceName)
	switch {
	case err == nil && up:
		return outcome{status: StatusUp, detail: DetailOK}
	case err == nil:
		return outcome{status: StatusDown, detail: DetailStatusMismatch}
	case errors.Is(err, ErrServiceNotFound):
		return outcome{status: StatusDown, detail: DetailNotFound}
	case errors.Is(err, ErrServiceDenied):
		return unknown(DetailDenied)
	case errors.Is(err, ErrServiceUnsupported):
		return unknown(DetailUnsupported)
	case errors.Is(err, context.DeadlineExceeded):
		return unknown(DetailTimeout)
	default:
		return unknown(DetailUnsupported)
	}
}

// latencyOutcome aplica los umbrales de latencia (0 = sin umbral): >=crit -> down,
// >=warn -> warn. El detalle sigue siendo ok: la sonda funciono, el estado marca la severidad.
func latencyOutcome(ms int64, warn, crit int) outcome {
	o := outcome{status: StatusUp, detail: DetailOK, ms: ms}
	switch {
	case crit > 0 && ms >= int64(crit):
		o.status = StatusDown
	case warn > 0 && ms >= int64(warn):
		o.status = StatusWarn
	}
	return o
}

// classify traduce un error de red al catalogo de detail. Nunca copia el texto del error.
func classify(err error) outcome {
	var (
		dnsErr    *net.DNSError
		netErr    net.Error
		certVerr  *tls.CertificateVerificationError
		unknownCA x509.UnknownAuthorityError
		hostErr   x509.HostnameError
		certInv   x509.CertificateInvalidError
		recHdr    tls.RecordHeaderError
	)
	switch {
	case errors.Is(err, errBlocked):
		return unknown(DetailBlocked)
	case errors.As(err, &certVerr), errors.As(err, &unknownCA), errors.As(err, &hostErr),
		errors.As(err, &certInv), errors.As(err, &recHdr):
		return outcome{status: StatusDown, detail: DetailTLS}
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return outcome{status: StatusDown, detail: DetailNotFound}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return outcome{status: StatusDown, detail: DetailTimeout}
	default:
		// Conexion rechazada u otro fallo de conexion (red inalcanzable, reset, DNS caido).
		return outcome{status: StatusDown, detail: DetailRefused}
	}
}
