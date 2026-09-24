// Package enroll canjea un enroll token por las credenciales del agente.
package enroll

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const (
	enrollPath = "/api/v1/agent/enroll"
	// maxRespBytes acota la respuesta del servidor (la real ocupa ~150 bytes).
	maxRespBytes = 64 << 10
)

// Errores que el llamador puede distinguir (con errors.Is).
var (
	// ErrInvalidToken: el servidor no reconoce el enroll token (inexistente, usado o expirado).
	ErrInvalidToken = errors.New("enroll token invalido, usado o expirado: pide uno nuevo en el dashboard")
	// ErrRateLimited: demasiados intentos de enroll desde esta IP.
	ErrRateLimited = errors.New("demasiados intentos de enroll: espera un momento y reintenta")
)

// Result es lo que devuelve el servidor. AgentToken es secreto.
type Result struct {
	HostID     string
	AgentToken string
}

type request struct {
	EnrollToken string `json:"enroll_token"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

type response struct {
	Data *struct {
		HostID     string `json:"host_id"`
		AgentToken string `json:"agent_token"`
	} `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

// Do llama a POST /api/v1/agent/enroll. serverURL es la URL base. Los errores
// nunca incluyen el enroll token ni el agent token.
func Do(ctx context.Context, client *http.Client, serverURL, enrollToken, osName, arch string) (Result, error) {
	endpoint, err := url.JoinPath(serverURL, enrollPath)
	if err != nil {
		return Result{}, fmt.Errorf("construyendo url del enroll: %w", err)
	}
	body, err := json.Marshal(request{EnrollToken: enrollToken, OS: osName, Arch: arch})
	if err != nil {
		return Result{}, fmt.Errorf("serializando enroll: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("creando request de enroll: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("llamando al servidor: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // lectura acotada abajo; un error al cerrar no aporta nada

	var env response
	decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxRespBytes)).Decode(&env)

	switch resp.StatusCode {
	case http.StatusCreated, http.StatusOK:
		if decodeErr != nil || env.Data == nil || env.Data.HostID == "" || env.Data.AgentToken == "" {
			return Result{}, errors.New("respuesta de enroll incompleta o ilegible")
		}
		return Result{HostID: env.Data.HostID, AgentToken: env.Data.AgentToken}, nil
	case http.StatusUnauthorized:
		return Result{}, ErrInvalidToken
	case http.StatusTooManyRequests:
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil && secs > 0 {
				return Result{}, fmt.Errorf("%w (Retry-After: %ds)", ErrRateLimited, secs)
			}
		}
		return Result{}, ErrRateLimited
	default:
		code := ""
		if env.Error != nil {
			code = env.Error.Code
		}
		if code != "" {
			return Result{}, fmt.Errorf("el servidor rechazo el enroll: %d %s", resp.StatusCode, code)
		}
		return Result{}, fmt.Errorf("el servidor rechazo el enroll: %d", resp.StatusCode)
	}
}
