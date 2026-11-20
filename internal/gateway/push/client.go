package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/Gtollm/TripGO/internal/config"
)

const (
	pushEndpointPath       = "/api/v1/push"
	maxResponseBodySnippet = 4 << 10
)

var (
	ErrClientError = errors.New("push service client error")
	ErrServerError = errors.New("push service server error")
)

type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("push service returned status %d", e.StatusCode)
	}

	return fmt.Sprintf("push service returned status %d: %s", e.StatusCode, e.Body)
}

func (e *StatusError) Unwrap() error {
	if e.StatusCode >= http.StatusBadRequest && e.StatusCode < http.StatusInternalServerError {
		return ErrClientError
	}

	if e.StatusCode >= http.StatusInternalServerError && e.StatusCode <= 599 {
		return ErrServerError
	}

	return nil
}

type pushRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data,omitempty"`
}

type Client struct {
	endpoint   string
	transport  *http.Transport
	httpClient *http.Client
}

func New(
	cfg config.PushConfig,
	tracerProvider trace.TracerProvider,
	propagator propagation.TextMapPropagator,
) *Client {
	baseTransport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   cfg.RequestTimeout,
			KeepAlive: cfg.KeepAlive,
		}).DialContext,
		TLSHandshakeTimeout:   cfg.RequestTimeout,
		ResponseHeaderTimeout: cfg.RequestTimeout,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConns,
		IdleConnTimeout:       cfg.IdleConnTimeout,
	}

	opts := []otelhttp.Option{
		otelhttp.WithMeterProvider(noop.NewMeterProvider()),
		otelhttp.WithSpanNameFormatter(
			func(_ string, r *http.Request) string {
				return r.Method + " " + pushEndpointPath
			},
		),
	}
	if tracerProvider != nil {
		opts = append(opts, otelhttp.WithTracerProvider(tracerProvider))
	}
	if propagator != nil {
		opts = append(opts, otelhttp.WithPropagators(propagator))
	}

	return &Client{
		endpoint:  strings.TrimRight(cfg.HTTPURL, "/") + pushEndpointPath,
		transport: baseTransport,
		httpClient: &http.Client{
			Transport: otelhttp.NewTransport(baseTransport, opts...),
			Timeout:   cfg.RequestTimeout,
		},
	}
}

func (c *Client) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	payload, err := json.Marshal(
		pushRequest{
			RequestID:   uuid.NewString(),
			RecipientID: driverID,
			Kind:        "REQUEST_POSITION",
			Data:        map[string]string{"trip_id": tripID.String()},
		},
	)
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send push request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBodySnippet))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusAccepted {
		return nil
	}

	bodyBytes, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySnippet))
	bodyText := strings.TrimSpace(string(bodyBytes))
	if readErr != nil && bodyText == "" {
		bodyText = readErr.Error()
	}

	return &StatusError{
		StatusCode: resp.StatusCode,
		Body:       bodyText,
	}
}

func (c *Client) Close() {
	c.transport.CloseIdleConnections()
}
