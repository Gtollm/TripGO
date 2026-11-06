package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/Gtollm/TripGO/internal/http/requeststate"
)

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := requeststate.From(r.Context())
			state.Complete = append(state.Complete, func() { logRequest(logger, r.Method, state) })
			next.ServeHTTP(w, r)
		})
	}
}

func logRequest(logger *slog.Logger, method string, state *requeststate.State) {
	status := state.Status()
	level := slog.LevelInfo
	attrs := []slog.Attr{
		slog.String("method", method), slog.String("route", state.Route),
		slog.Int("status", status), slog.Float64("duration_ms", float64(time.Since(state.Started))/float64(time.Millisecond)),
	}

	if status >= http.StatusInternalServerError || len(state.Stack) != 0 {
		level = slog.LevelError
		if state.Err != nil {
			attrs = append(attrs, slog.Any("error", state.Err))
		}
	}

	if len(state.Stack) != 0 {
		attrs = append(attrs, slog.String("stack", string(state.Stack)))
	}

	logger.LogAttrs(state.Context, level, "request handled", attrs...)
}
