package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/Gtollm/TripGO/internal/http/requeststate"
	"github.com/Gtollm/TripGO/internal/http/response"
)

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writer := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			state := &requeststate.State{
				Context:   r.Context(),
				Started:   time.Now(),
				Committed: func() bool { return writer.Status() != 0 },
				Status: func() int {
					if status := writer.Status(); status != 0 {
						return status
					}

					return http.StatusOK
				},
			}

			r = r.WithContext(requeststate.With(r.Context(), state))
			defer func() {
				if value := recover(); value != nil {
					if recoverPanic(writer, r, value) {
						defer func() { panic(http.ErrAbortHandler) }()
					}
				}

				if len(state.Complete) == 0 {
					logRequest(logger, r.Method, state)
				}

				for i := len(state.Complete) - 1; i >= 0; i-- {
					state.Complete[i]()
				}
			}()

			next.ServeHTTP(writer, r)
		})
	}
}

func recoverPanic(w http.ResponseWriter, r *http.Request, value any) bool {
	if value == http.ErrAbortHandler {
		return true
	}
	state := requeststate.From(r.Context())
	state.Stack = debug.Stack()
	err := fmt.Errorf("panic recovered: %v", value)
	if !state.Committed() {
		response.ToError(w, r, err)
		return false
	} else {
		requeststate.Fail(r.Context(), err, "internal_error")
		return true
	}
}
