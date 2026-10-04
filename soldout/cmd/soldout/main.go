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

	bookinghttp "github.com/nikolaysavelev/soldout/internal/booking/adapters/http"
	bookingpg "github.com/nikolaysavelev/soldout/internal/booking/adapters/pg"
	bookingapp "github.com/nikolaysavelev/soldout/internal/booking/app"
	cataloghttp "github.com/nikolaysavelev/soldout/internal/catalog/adapters/http"
	catalogpg "github.com/nikolaysavelev/soldout/internal/catalog/adapters/pg"
	catalogapp "github.com/nikolaysavelev/soldout/internal/catalog/app"
	notificationlog "github.com/nikolaysavelev/soldout/internal/notification/adapters/log"
	notificationpg "github.com/nikolaysavelev/soldout/internal/notification/adapters/pg"
	notificationapp "github.com/nikolaysavelev/soldout/internal/notification/app"
	paymentpg "github.com/nikolaysavelev/soldout/internal/payment/adapters/pg"
	paymentpsp "github.com/nikolaysavelev/soldout/internal/payment/adapters/psp"
	paymentapp "github.com/nikolaysavelev/soldout/internal/payment/app"
	"github.com/nikolaysavelev/soldout/internal/platform/cache"
	"github.com/nikolaysavelev/soldout/internal/platform/config"
	"github.com/nikolaysavelev/soldout/internal/platform/db"
	"github.com/nikolaysavelev/soldout/internal/platform/faults"
	"github.com/nikolaysavelev/soldout/internal/platform/httpx"
	platformlog "github.com/nikolaysavelev/soldout/internal/platform/log"
	"github.com/nikolaysavelev/soldout/internal/platform/metrics"
	queuehttp "github.com/nikolaysavelev/soldout/internal/queue/adapters/http"
	queuepg "github.com/nikolaysavelev/soldout/internal/queue/adapters/pg"
	queuevalkey "github.com/nikolaysavelev/soldout/internal/queue/adapters/valkey"
	queueapp "github.com/nikolaysavelev/soldout/internal/queue/app"
	ticketinghttp "github.com/nikolaysavelev/soldout/internal/ticketing/adapters/http"
	ticketingpg "github.com/nikolaysavelev/soldout/internal/ticketing/adapters/pg"
	ticketingapp "github.com/nikolaysavelev/soldout/internal/ticketing/app"
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
	flags, err := faults.FromEnv()
	if err != nil {
		return err
	}
	logger := platformlog.New(cfg.LogLevel, cfg.LogFormat)
	logger.Info("старт soldout", "addr", cfg.HTTPAddr, "hold_ttl", cfg.HoldTTL.String(), "db_max_conns", cfg.DBMaxConns,
		"psp_fail_rate", flags.PSPFailRate, "psp_delay", flags.SlowPSP.String())

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
	bookingStore := bookingpg.New(pool)
	catalogSvc := catalogapp.New(catalogpg.New(pool), bookingStore) // статусы мест — из booking
	queueSvc := queueapp.New(queuepg.New(pool), queuevalkey.New(vk), catalogSvc, cfg.AdmissionTTL, logger)
	paymentSvc := paymentapp.New(paymentpsp.New(flags.PSPFailRate, flags.SlowPSP), paymentpg.New(pool))
	notificationSvc := notificationapp.New(notificationlog.New(logger), notificationpg.New(pool))

	ticketingSvc := ticketingapp.New(ticketingpg.New(pool), notificationSvc, logger)
	bookingSvc := bookingapp.New(bookingStore, catalogSvc, queueSvc, paymentSvc, ticketingSvc,
		bookingapp.Options{HoldTTL: cfg.HoldTTL, TicketPriceMinor: cfg.TicketPriceMinor, Faults: flags}, logger)

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
		bookinghttp.New(bookingSvc).Mount(r)
		ticketinghttp.New(ticketingSvc, bookingSvc).Mount(r)
	})

	srv := &http.Server{
		Addr: cfg.HTTPAddr, Handler: r,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}

	// --- фон: expirer ---
	expirerDone := make(chan struct{})
	go func() {
		defer close(expirerDone)
		bookingapp.NewExpirer(bookingStore, cfg.ExpirerInterval, m.HoldsActive, logger).Run(ctx)
	}()

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
	<-expirerDone
	logger.Info("soldout остановлен")
	return nil
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
