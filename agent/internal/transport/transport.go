// Package transport construye el unico http.Client del agente.
package transport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// NewClient devuelve el cliente HTTP compartido por enroll y metricas.
//   - caFile vacio: se usan las CAs del sistema. Con caFile: SOLO esa CA (pool
//     propio, no se mezcla con las del sistema), pa fijar una CA privada.
//   - Nunca se desactiva la verificacion del certificado.
//   - No sigue redirecciones: la API no las usa y seguir una podria mandar el
//     bearer token a otro destino (o degradar a http); un 3xx cuenta como error.
func NewClient(caFile string, timeout time.Duration) (*http.Client, error) {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile) //nolint:gosec // ruta elegida por el operador via ARGOS_CA_FILE
		if err != nil {
			return nil, fmt.Errorf("leyendo ARGOS_CA_FILE: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("ARGOS_CA_FILE no contiene ningun certificado PEM valido")
		}
		tlsCfg.RootCAs = pool
	}
	tr.TLSClientConfig = tlsCfg
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}
