package otel

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Handler оборачивает HTTP-сервер: серверный span на каждый входящий запрос с извлечением traceparent.
// Служебные маршруты (/metrics, /healthz, /readyz, /debug/pprof) не трассируются: это шум, а не запросы пользователей.
func Handler(h http.Handler, service string) http.Handler {
	return otelhttp.NewHandler(h, service,
		otelhttp.WithFilter(func(r *http.Request) bool {
			p := r.URL.Path
			return p != "/metrics" && p != "/healthz" && p != "/readyz" && !strings.HasPrefix(p, "/debug/")
		}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }),
	)
}

// NameByRoute — middleware для chi: после маршрутизации переименовывает серверный span по шаблону маршрута
// ("POST /v1/orders/{id}/pay", а не с конкретным id) и ставит http.route. Иначе у каждого заказа своё имя span'а
// и агрегаты (spanmetrics, TraceQL по имени) бессмысленны.
func NameByRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		span := trace.SpanFromContext(r.Context())
		if !span.IsRecording() {
			return
		}
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		span.SetName(r.Method + " " + route)
		span.SetAttributes(attribute.String("http.route", route))
	})
}

type attemptKey struct{}

// WithAttempt помечает исходящий вызов номером попытки (0 — первая). Transport запишет retry.attempt в span.
func WithAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptKey{}, attempt)
}

// Transport — клиентский транспорт: span на каждый исходящий HTTP-вызов ("<peer>.POST /send") и traceparent
// в заголовках. Повтор — отдельный span с атрибутом retry.attempt.
func Transport(base http.RoundTripper, peer string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(attemptTransport{base: base},
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return peer + "." + r.Method + " " + r.URL.Path }),
	)
}

// attemptTransport работает внутри otelhttp: в ctx запроса уже лежит клиентский span, дописываем ему номер попытки.
type attemptTransport struct{ base http.RoundTripper }

func (t attemptTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if n, ok := r.Context().Value(attemptKey{}).(int); ok {
		trace.SpanFromContext(r.Context()).SetAttributes(attribute.Int("retry.attempt", n))
	}
	return t.base.RoundTrip(r)
}
