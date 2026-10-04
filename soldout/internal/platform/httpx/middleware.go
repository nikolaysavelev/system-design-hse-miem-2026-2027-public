package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/nikolaysavelev/soldout/internal/platform/metrics"
)

type loggerKey struct{}

// WithLogger кладёт логгер в контекст запроса; используйте LoggerFrom в хендлерах.
func WithLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			l := logger.With("request_id", middleware.GetReqID(r.Context()))
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), loggerKey{}, l)))
		})
	}
}

// LoggerFrom возвращает логгер запроса (или slog.Default()).
func LoggerFrom(ctx context.Context) *slog.Logger { return logFromContext(ctx) }

func logFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// Instrument — RED-метрики и структурный access-лог по каждому запросу. Маршрут берётся из chi после роутинга.
func Instrument(m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			status := strconv.Itoa(ww.Status())
			elapsed := time.Since(start)
			m.HTTPRequests.WithLabelValues(r.Method, route, status).Inc()
			m.HTTPDuration.WithLabelValues(r.Method, route, status).Observe(elapsed.Seconds())
			logFromContext(r.Context()).InfoContext(r.Context(), "http",
				"method", r.Method, "route", route, "path", r.URL.Path, "status", ww.Status(),
				"bytes", ww.BytesWritten(), "duration_ms", float64(elapsed.Microseconds())/1000)
		})
	}
}

// Recover превращает панику в 500 и пишет стек в лог.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				logFromContext(r.Context()).ErrorContext(r.Context(), "panic", "panic", p, "stack", string(debug.Stack()))
				JSON(w, http.StatusInternalServerError, map[string]any{"error": &Error{Code: "internal", Message: "внутренняя ошибка"}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Timeout ограничивает время обработки запроса через контекст (без обрыва ответа).
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
