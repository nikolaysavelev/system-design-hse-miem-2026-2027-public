// Command soldout — модульный монолит «билетного дропа»: сборка зависимостей, HTTP-сервер, graceful shutdown.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	bookingapi "github.com/nikolaysavelev/soldout/internal/booking/api"
	cataloghttp "github.com/nikolaysavelev/soldout/internal/catalog/adapters/http"
	catalogpg "github.com/nikolaysavelev/soldout/internal/catalog/adapters/pg"
	catalogapp "github.com/nikolaysavelev/soldout/internal/catalog/app"
	"github.com/nikolaysavelev/soldout/internal/platform/cache"
	"github.com/nikolaysavelev/soldout/internal/platform/config"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/platform/httpx"
	platformlog "github.com/nikolaysavelev/soldout/internal/platform/log"
	"github.com/nikolaysavelev/soldout/internal/platform/metrics"
	queuehttp "github.com/nikolaysavelev/soldout/internal/queue/adapters/http"
	queuepg "github.com/nikolaysavelev/soldout/internal/queue/adapters/pg"
	queuevalkey "github.com/nikolaysavelev/soldout/internal/queue/adapters/valkey"
	queueapp "github.com/nikolaysavelev/soldout/internal/queue/app"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "проверить /readyz работающего процесса и выйти (для HEALTHCHECK в distroless)")
	flag.Parse()
	if *healthcheck {
		os.Exit(runHealthcheck())
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "soldout:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := platformlog.New(cfg.LogLevel, cfg.LogFormat)
	logger.Info("старт soldout", "addr", cfg.HTTPAddr, "hold_ttl", cfg.HoldTTL.String(), "db_max_conns", cfg.DBMaxConns)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- платформа ---
	m := metrics.New()
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, cfg.DatabaseURL, logger); err != nil {
			return err
		}
	}
	pool, err := db.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns, m)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.SeedOnStart {
		if err := db.Seed(ctx, pool, cfg.SeedSeatsPerEvent, logger); err != nil {
			return err
		}
	}
	vk, err := cache.New(ctx, cfg.ValkeyAddr)
	if err != nil {
		return err
	}
	defer vk.Close()

	// --- модули: сборка по правилу зависимостей (см. .go-arch-lint.yml) ---
	// Акт 2: модуль booking (domain, app, adapters) ещё не реализован — см. lessons/01/demo.md.
	// Пока карта зала получает пустые статусы мест через заглушку; после реализации сюда
	// подставляется bookingpg.New(pool) и сервисы payment/ticketing/notification.
	catalogSvc := catalogapp.New(catalogpg.New(pool), noBooking{})
	queueSvc := queueapp.New(queuepg.New(pool), queuevalkey.New(vk), catalogSvc, cfg.AdmissionTTL, logger)

	// --- HTTP ---
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, httpx.WithLogger(logger), httpx.Recover)
	r.Get("/healthz", httpx.Healthz)
	r.Get("/readyz", httpx.Readyz(map[string]httpx.Checker{
		"postgres": pool.Ping,
		"valkey":   func(ctx context.Context) error { return cache.Ping(ctx, vk) },
	}))
	r.Handle("/metrics", m.Handler())
	r.Route("/debug/pprof", func(r chi.Router) {
		r.Get("/", pprof.Index)
		r.Get("/cmdline", pprof.Cmdline)
		r.Get("/profile", pprof.Profile)
		r.Get("/symbol", pprof.Symbol)
		r.Get("/trace", pprof.Trace)
		r.Get("/{name}", func(w http.ResponseWriter, req *http.Request) {
			pprof.Handler(chi.URLParam(req, "name")).ServeHTTP(w, req)
		})
	})
	r.Group(func(r chi.Router) {
		r.Use(httpx.Instrument(m), httpx.Timeout(30*time.Second))
		cataloghttp.New(catalogSvc).Mount(r)
		queuehttp.New(queueSvc).Mount(r)
		// TODO(акт 2): bookinghttp.New(bookingSvc).Mount(r); ticketinghttp.New(ticketingSvc, bookingSvc).Mount(r)
	})

	srv := &http.Server{
		Addr: cfg.HTTPAddr, Handler: r,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP-сервер слушает", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	logger.Info("остановка: ждём завершения запросов", "timeout", cfg.ShutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP-сервер остановлен принудительно", "err", err)
	}
	logger.Info("soldout остановлен")
	return nil
}

// noBooking — заглушка до реализации модуля booking (акт 2): все места свободны.
type noBooking struct{}

func (noBooking) SeatStates(context.Context, uuid.UUID) (map[int64]bookingapi.SeatState, error) {
	return map[int64]bookingapi.SeatState{}, nil
}

func runHealthcheck() int {
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		return 1
	}
	return 0
}
