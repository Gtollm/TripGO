package container_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"

	"github.com/Gtollm/TripGO/internal/http/middleware"
)

func TestRecover(t *testing.T) {
	t.Parallel()

	router := chi.NewRouter()
	router.Get("/test", func(http.ResponseWriter, *http.Request) {
		panic("private panic details")
	})

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	metrics, err := middleware.NewMetrics(noop.NewMeterProvider().Meter("test"))
	require.NoError(t, err)

	handler := middleware.Recover(logger)(
		middleware.Tracing(tracenoop.NewTracerProvider(), propagation.TraceContext{}, router)(
			middleware.Logging(logger)(metrics.Wrap(router)),
		),
	)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/test", nil))

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Contains(t, response.Body.String(), "internal_error")
	require.NotContains(t, response.Body.String(), "private panic details")
}

func TestPanicAfterResponseStartedAbortsResponse(t *testing.T) {
	t.Parallel()
	for _, value := range []any{"broken response", http.ErrAbortHandler} {
		router := chi.NewRouter()
		router.Get("/partial", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("partial"))
			w.(http.Flusher).Flush()
			panic(value)
		})
		logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
		handler := middleware.Recover(logger)(middleware.Tracing(tracenoop.NewTracerProvider(), propagation.TraceContext{}, router)(router))
		server := httptest.NewServer(handler)
		func() {
			defer server.Close()
			resp, err := server.Client().Get(server.URL + "/partial")
			require.NoError(t, err)
			defer resp.Body.Close()
			_, err = io.ReadAll(resp.Body)
			require.Error(t, err)
		}()
	}
}
