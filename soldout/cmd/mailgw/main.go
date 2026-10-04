// Command mailgw — эмулятор внешнего почтового шлюза (занятие 4).
//
// POST /send принимает JSON {to, subject, body}, спит MAILGW_DELAY_MS ± 30 % и с вероятностью MAILGW_FAIL_RATE
// отвечает 502. Так выглядит реальный провайдер: медленно и иногда 5xx. Базы нет; метрики — /metrics.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/nikolaysavelev/soldout/internal/platform/otel"
)

type config struct {
	addr     string
	delay    time.Duration
	jitter   float64 // доля разброса задержки: 0.3 = ±30 %
	failRate float64
}

type message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type server struct {
	cfg      config
	logger   *slog.Logger
	requests *prometheus.CounterVec
	duration prometheus.Histogram
	rnd      func() float64
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "проверить /healthz работающего процесса и выйти")
	flag.Parse()
	cfg, err := fromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mailgw:", err)
		os.Exit(1)
	}
	if *healthcheck {
		os.Exit(runHealthcheck(cfg.addr))
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "mailgw:", err)
		os.Exit(1)
	}
}

func fromEnv() (config, error) {
	c := config{addr: ":8090", delay: 600 * time.Millisecond, jitter: 0.3, failRate: 0.05}
	if v := os.Getenv("MAILGW_ADDR"); v != "" {
		c.addr = v
	}
	if v := os.Getenv("MAILGW_DELAY_MS"); v != "" {
		ms, err := strconv.Atoi(v)
		if err != nil || ms < 0 {
			return c, fmt.Errorf("MAILGW_DELAY_MS=%q: ожидается число миллисекунд", v)
		}
		c.delay = time.Duration(ms) * time.Millisecond
	}
	if v := os.Getenv("MAILGW_FAIL_RATE"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 || f > 1 {
			return c, fmt.Errorf("MAILGW_FAIL_RATE=%q: ожидается число 0..1", v)
		}
		c.failRate = f
	}
	return c, nil
}

func run(cfg config) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	// серверный span POST /send (service.name=mailgw): в трейсе pay видно, что время уходит на стороне шлюза
	shutdownTracing, err := otel.Init(context.Background(), otel.Options{
		ServiceName: "mailgw", ServiceVersion: "emulator", Environment: "lab",
		Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), SampleRatio: 1,
	})
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(context.Background()) }()
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	s := &server{
		cfg: cfg, logger: logger, rnd: rand.Float64,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mailgw_requests_total", Help: "Запросы к почтовому шлюзу по статусу ответа.",
		}, []string{"status"}),
		duration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "mailgw_request_duration_seconds", Help: "Время ответа почтового шлюза.",
			Buckets: []float64{.1, .25, .5, .75, 1, 1.5, 2, 3},
		}),
	}
	reg.MustRegister(s.requests, s.duration)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /send", s.send)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	srv := &http.Server{Addr: cfg.addr, Handler: otel.Handler(mux, "mailgw"), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	logger.Info("mailgw слушает", "addr", cfg.addr, "delay", cfg.delay.String(), "fail_rate", cfg.failRate)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *server) send(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var m message
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil || m.To == "" {
		s.finish(w, start, http.StatusBadRequest, "bad_request")
		return
	}
	// задержка delay ± jitter; клиент ушёл — не спим дальше
	d := time.Duration(float64(s.cfg.delay) * (1 + s.cfg.jitter*(2*s.rnd()-1)))
	select {
	case <-r.Context().Done():
		s.requests.WithLabelValues("canceled").Inc()
		return
	case <-time.After(d):
	}
	if s.cfg.failRate > 0 && s.rnd() < s.cfg.failRate {
		otel.SetAttributes(r.Context(), otel.Bool("mailgw.injected_failure", true))
		s.finish(w, start, http.StatusBadGateway, "upstream_unavailable")
		return
	}
	s.finish(w, start, http.StatusAccepted, "")
}

func (s *server) finish(w http.ResponseWriter, start time.Time, status int, code string) {
	s.requests.WithLabelValues(strconv.Itoa(status)).Inc()
	s.duration.Observe(time.Since(start).Seconds())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if code != "" {
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
}

func runHealthcheck(addr string) int {
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
