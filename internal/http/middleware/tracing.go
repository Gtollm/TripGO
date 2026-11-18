package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/Gtollm/TripGO/internal/http/requeststate"
)

func Tracing(provider trace.TracerProvider, propagator propagation.TextMapPropagator, routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		instrumented := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := requeststate.From(r.Context())
			state.Context = r.Context()
			span := trace.SpanFromContext(r.Context())
			if state.Route != "" {
				span.SetAttributes(attribute.String("http.route", state.Route))
			}
			defer func() {
				if value := recover(); value != nil {
					if recoverPanic(w, r, value) {
						defer func() { panic(http.ErrAbortHandler) }()
					}
				}

				if state.Err != nil {
					span.RecordError(state.Err)
					span.SetStatus(codes.Error, state.ErrorType)
					span.SetAttributes(attribute.String("error.type", state.ErrorType))
				}
			}()
			next.ServeHTTP(w, r)
		}), "http request", otelhttp.WithTracerProvider(provider), otelhttp.WithPropagators(propagator),
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				if route := requeststate.From(r.Context()).Route; route != "" {
					return r.Method + " " + route
				}

				return r.Method
			}))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.RawPath
			if path == "" {
				path = r.URL.Path
			}

			state := requeststate.From(r.Context())
			state.Route = routes.Find(chi.NewRouteContext(), r.Method, path)
			instrumented.ServeHTTP(w, r)
		})
	}
}
