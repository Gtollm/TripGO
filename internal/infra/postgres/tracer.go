package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

type (
	queryKey  struct{}
	queryInfo struct {
		started               time.Time
		operation, collection string
	}
)

type Tracer struct {
	*otelpgx.Tracer
	duration metric.Float64Histogram
}

func NewTracer(provider trace.TracerProvider, meter metric.Meter) (*Tracer, error) {
	duration, err := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("db duration metric: %w", err)
	}

	return &Tracer{Tracer: otelpgx.NewTracer(otelpgx.WithTracerProvider(provider),
		otelpgx.WithMeterProvider(noop.NewMeterProvider()), otelpgx.WithDisableAcquireTracer(),
		otelpgx.WithDisableConnectionDetailsInAttributes(), otelpgx.WithDisableSQLStatementInAttributes()), duration: duration}, nil
}

func (t *Tracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	info := queryInfo{
		started:    time.Now(),
		operation:  otelpgx.SQLOperationName(ctx, data.SQL),
		collection: collectionName(data.SQL),
	}

	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attribute.String("db.operation.name", info.operation), attribute.String("db.collection.name", info.collection))
	return context.WithValue(ctx, queryKey{}, info)
}

func (t *Tracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if info, ok := ctx.Value(queryKey{}).(queryInfo); ok {
		attrs := []attribute.KeyValue{attribute.String("db.operation.name", info.operation), attribute.String("db.collection.name", info.collection)}
		if data.Err != nil {
			kind := errorType(data.Err)
			attrs = append(attrs, attribute.String("error.type", kind))
			trace.SpanFromContext(ctx).SetAttributes(attribute.String("error.type", kind))
		}

		t.duration.Record(ctx, time.Since(info.started).Seconds(), metric.WithAttributes(attrs...))
	}

	t.Tracer.TraceQueryEnd(ctx, conn, data)
}

func collectionName(sql string) string {
	words := strings.Fields(sql)
	for i := 0; i+1 < len(words); i++ {
		switch strings.ToUpper(words[i]) {
		case "FROM", "INTO", "UPDATE":
			table := strings.ToLower(strings.Trim(words[i+1], `"`))
			switch table {
			case "trips", "trip_status_history", "idempotency_keys":
				return table
			}
		}
	}

	return ""
}

func errorType(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		return pgErr.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "database_error"
	}
}
