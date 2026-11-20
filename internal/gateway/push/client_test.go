package push_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/goleak"

	"github.com/Gtollm/TripGO/internal/config"
	"github.com/Gtollm/TripGO/internal/gateway/push"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func pushConfig(url string, timeout time.Duration) config.PushConfig {
	return config.PushConfig{
		HTTPURL:         url,
		RequestTimeout:  timeout,
		KeepAlive:       30 * time.Second,
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}
}

func TestRequestPositionSuccess(t *testing.T) {
	t.Parallel()

	type captured struct {
		method, path, trace string
		err                 error
		RequestID           string            `json:"request_id"`
		RecipientID         uuid.UUID         `json:"recipient_id"`
		Kind                string            `json:"kind"`
		Data                map[string]string `json:"data"`
	}

	tripID, driverID := uuid.New(), uuid.New()
	received := make(chan captured, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := captured{
			method: r.Method,
			path:   r.URL.Path,
			trace:  r.Header.Get("Traceparent"),
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			got.err = err
		} else {
			got.err = json.Unmarshal(body, &got)
		}
		received <- got
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter()))
	defer func() { require.NoError(t, tp.Shutdown(t.Context())) }()

	client := push.New(pushConfig(srv.URL, time.Second), tp, propagation.TraceContext{})
	defer client.Close()

	ctx, span := tp.Tracer("test").Start(t.Context(), "root")
	err := client.RequestPosition(ctx, tripID, driverID)
	span.End()

	require.NoError(t, err)
	got := <-received
	require.NoError(t, got.err)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/api/v1/push", got.path)
	require.NotEmpty(t, got.trace)
	require.NotEmpty(t, got.RequestID)
	require.Equal(t, driverID, got.RecipientID)
	require.Equal(t, "REQUEST_POSITION", got.Kind)
	require.Equal(t, map[string]string{"trip_id": tripID.String()}, got.Data)
}

func TestRequestPositionErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		target error
	}{
		{"4xx", http.StatusBadRequest, push.ErrClientError},
		{"5xx", http.StatusServiceUnavailable, push.ErrServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			client := push.New(pushConfig(srv.URL, time.Second), nil, nil)
			defer client.Close()

			require.ErrorIs(t, client.RequestPosition(t.Context(), uuid.New(), uuid.New()), tc.target)
		})
	}

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-release:
			case <-r.Context().Done():
			}
		}))
		defer srv.Close()
		defer close(release)

		client := push.New(pushConfig(srv.URL, 25*time.Millisecond), nil, nil)
		defer client.Close()

		err := client.RequestPosition(t.Context(), uuid.New(), uuid.New())
		require.Error(t, err)
		require.False(t, errors.Is(err, push.ErrClientError))
		require.False(t, errors.Is(err, push.ErrServerError))
	})
}
