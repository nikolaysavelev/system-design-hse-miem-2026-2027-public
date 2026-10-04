package httpx

import (
	"context"
	"net/http"
	"time"
)

// Checker — проверка зависимости для /readyz.
type Checker func(ctx context.Context) error

// Healthz — процесс жив.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readyz — все зависимости отвечают; иначе 503 со списком.
func Readyz(checks map[string]Checker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		result := map[string]string{}
		ok := true
		for name, check := range checks {
			if err := check(ctx); err != nil {
				result[name] = err.Error()
				ok = false
			} else {
				result[name] = "ok"
			}
		}
		status := http.StatusOK
		if !ok {
			status = http.StatusServiceUnavailable
		}
		JSON(w, status, map[string]any{"ready": ok, "checks": result})
	}
}
