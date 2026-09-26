package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

type envelope struct {
	Data  any       `json:"data"`
	Error *apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Details son datos estructurados opcionales del error (p. ej. lo que se perderia en shrink_needs_confirm).
	Details any `json:"details,omitempty"`
}

// clientError es un error del cliente (4xx) con su respuesta ya definida.
type clientError struct {
	status int
	code   string
	msg    string
	// retryAfter, si es > 0, se envia como cabecera Retry-After (429).
	retryAfter time.Duration
	// details, si no es nil, se envia en error.details.
	details any
}

func (e *clientError) Error() string { return e.code + ": " + e.msg }

func badRequest(format string, args ...any) error {
	return &clientError{status: http.StatusBadRequest, code: "invalid_body", msg: fmt.Sprintf(format, args...)}
}

// Errores de cliente reutilizables. Son valores de solo lectura: no se mutan.
var (
	errUnauthenticated = &clientError{status: http.StatusUnauthorized, code: "unauthenticated", msg: "sesion no valida o expirada"}
	errForbidden       = &clientError{status: http.StatusForbidden, code: "forbidden", msg: "no tienes permiso para esta accion"}
	errMustChangePass  = &clientError{status: http.StatusForbidden, code: "password_change_required", msg: "debes cambiar tu contraseña antes de continuar"}
	// errInvalidCreds es la unica respuesta de un login fallido: no distingue usuario
	// inexistente, contraseña incorrecta, cuenta bloqueada o deshabilitada.
	errInvalidCreds = &clientError{status: http.StatusUnauthorized, code: "invalid_credentials", msg: "usuario o contraseña incorrectos"}
	// errInvalidAgentToken es la unica respuesta pa un token de agente o de enroll
	// inexistente, usado, expirado o revocado (el motivo real solo va al log).
	errInvalidAgentToken = &clientError{status: http.StatusUnauthorized, code: "invalid_token", msg: "token invalido o revocado"}
)

func rateLimited(retryAfter time.Duration) error {
	return &clientError{status: http.StatusTooManyRequests, code: "rate_limited",
		msg: "demasiadas solicitudes, intenta mas tarde", retryAfter: retryAfter}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// El status ya se envio: si falla la escritura (cliente desconectado) no hay nada util que hacer.
	_ = json.NewEncoder(w).Encode(envelope{Data: data})
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeErrorDetails(w, status, code, msg, nil)
}

func writeErrorDetails(w http.ResponseWriter, status int, code, msg string, details any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Error: &apiError{Code: code, Message: msg, Details: details}})
}

// decodeJSON lee un unico documento JSON del body con tope de maxBytes.
// Con strict rechaza campos desconocidos. Devuelve un *clientError (400 o 413).
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64, strict bool) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	if strict {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		return bodyError(err)
	}
	// Nada mas despues del documento (io.EOF = ok).
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return badRequest("el body debe contener un solo documento JSON")
		}
		return bodyError(err)
	}
	return nil
}

func bodyError(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return &clientError{status: http.StatusRequestEntityTooLarge, code: "payload_too_large",
			msg: fmt.Sprintf("body excede %d bytes", tooBig.Limit)}
	}
	return badRequest("json invalido")
}

// checkText valida un campo de texto obligatorio: no vacio, hasta max caracteres
// y sin NUL (Postgres rechaza \x00 en TEXT y terminaria en 500).
func checkText(field, s string, max int) error {
	if s == "" {
		return badRequest("%s requerido", field)
	}
	if utf8.RuneCountInString(s) > max {
		return badRequest("%s excede %d caracteres", field, max)
	}
	for _, r := range s {
		if r == 0 {
			return badRequest("%s contiene caracteres invalidos", field)
		}
	}
	return nil
}
