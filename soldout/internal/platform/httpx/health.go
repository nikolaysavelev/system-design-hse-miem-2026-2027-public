package httpx

import (
	"context"
	"net/http"
	"runtime"
	"runtime/debug"
	"time"
)

// Checker — проверка зависимости для /readyz.
type Checker func(ctx context.Context) error

// Healthz — процесс жив.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Diagnostics — статические сведения о процессе для /readyz (проверка лимитов контейнера).
type Diagnostics map[string]any

// RuntimeDiagnostics — GOMAXPROCS (должен равняться cpus контейнера), GOMEMLIMIT, версия Go.
func RuntimeDiagnostics() Diagnostics {
	limit := debug.SetMemoryLimit(-1)
	var mem any = limit
	if limit == 1<<63-1 { // math.MaxInt64 = лимит не задан
		mem = "не задан"
	}
	return Diagnostics{
		"gomaxprocs": runtime.GOMAXPROCS(0),
		"numcpu":     runtime.NumCPU(),
		"gomemlimit": mem,
		"go":         runtime.Version(),
	}
}

// Readyz — все зависимости отвечают; иначе 503 со списком. diag добавляется в ответ как есть.
func Readyz(checks map[string]Checker, diag ...Diagnostics) http.HandlerFunc {
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
		body := map[string]any{"ready": ok, "checks": result}
		for _, d := range diag {
			for k, v := range d {
				body[k] = v
			}
		}
		JSON(w, status, body)
	}
}
