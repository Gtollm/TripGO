package ready_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Gtollm/TripGO/internal/http/health"
	"github.com/Gtollm/TripGO/internal/http/ready"
)

func TestReady(t *testing.T) {
	t.Parallel()

	for _, available := range []bool{true, false} {
		t.Run("availability", func(t *testing.T) {
			t.Parallel()

			probe := NewMockreadiness(gomock.NewController(t))
			probe.EXPECT().Ping(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.WithinDuration(t, time.Now().Add(time.Second), deadline, time.Second)
				if available {
					return nil
				}

				return errors.New("private db failure")
			})
			response := httptest.NewRecorder()
			ready.New(probe, time.Second).Handle(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
			if available {
				require.Equal(t, 200, response.Code)
				require.JSONEq(t, `{"status":"ok"}`, response.Body.String())
			} else {
				require.Equal(t, 503, response.Code)
				require.JSONEq(t, `{"status":"unavailable"}`, response.Body.String())
			}

			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
		})
	}
}

func TestHealth(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	health.Handle(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, 200, response.Code)
	require.JSONEq(t, `{"status":"ok"}`, response.Body.String())
}
