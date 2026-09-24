package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
)

// Ids de evento del registro de eventos de Windows (EventCreate acepta 1-1000).
const (
	eventIDError uint32 = 1
	eventIDWarn  uint32 = 2
	eventIDInfo  uint32 = 3
)

// EventSink es donde caen los eventos (lo implementa *eventlog.Log de x/sys/windows/svc/eventlog).
type EventSink interface {
	Info(eid uint32, msg string) error
	Warning(eid uint32, msg string) error
	Error(eid uint32, msg string) error
}

// NewEventLogHandler devuelve un slog.Handler que escribe cada registro en sink
// con la severidad que corresponde (un servicio de Windows no tiene consola). El
// texto es el formato key=value de slog, sin la hora (el registro de eventos ya la tiene).
func NewEventLogHandler(sink EventSink, level slog.Leveler) slog.Handler {
	buf := &bytes.Buffer{}
	inner := slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	return &eventHandler{inner: inner, buf: buf, mu: &sync.Mutex{}, sink: sink}
}

// eventHandler comparte buf y mu entre los handlers derivados (WithAttrs/WithGroup):
// todos formatean sobre el mismo buffer, uno a la vez.
type eventHandler struct {
	inner slog.Handler
	buf   *bytes.Buffer
	mu    *sync.Mutex
	sink  EventSink
}

func (h *eventHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *eventHandler) Handle(ctx context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.Reset()
	if err := h.inner.Handle(ctx, r); err != nil {
		return err //nolint:wrapcheck // handler de slog: el error del formateo se devuelve tal cual
	}
	msg := strings.TrimSpace(h.buf.String())
	switch {
	case r.Level >= slog.LevelError:
		return h.sink.Error(eventIDError, msg) //nolint:wrapcheck // idem
	case r.Level >= slog.LevelWarn:
		return h.sink.Warning(eventIDWarn, msg) //nolint:wrapcheck // idem
	default:
		return h.sink.Info(eventIDInfo, msg) //nolint:wrapcheck // idem
	}
}

func (h *eventHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &eventHandler{inner: h.inner.WithAttrs(attrs), buf: h.buf, mu: h.mu, sink: h.sink}
}

func (h *eventHandler) WithGroup(name string) slog.Handler {
	return &eventHandler{inner: h.inner.WithGroup(name), buf: h.buf, mu: h.mu, sink: h.sink}
}
