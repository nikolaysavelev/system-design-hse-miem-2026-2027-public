// Package metrics — реестр Prometheus и RED-метрики платформы.
package metrics

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics — набор метрик сервиса. Один экземпляр на процесс, передаётся через DI (без глобальных переменных).
type Metrics struct {
	registry *prometheus.Registry

	HTTPRequests    *prometheus.CounterVec
	HTTPDuration    *prometheus.HistogramVec
	DBQueryDuration *prometheus.HistogramVec
	HoldsActive     prometheus.Gauge
}

// New создаёт реестр и регистрирует метрики процесса, Go-рантайма и RED-метрики.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,
		HTTPRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Количество HTTP-запросов по методу, маршруту и статусу.",
		}, []string{"method", "route", "status"}),
		HTTPDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Длительность HTTP-запросов.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"method", "route", "status"}),
		DBQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Длительность SQL-запросов по имени запроса (комментарий /* name */ в начале SQL).",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}, []string{"query"}),
		HoldsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "holds_active",
			Help: "Число активных hold (обновляется expirer'ом).",
		}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.DBQueryDuration, m.HoldsActive,
	)
	return m
}

// Handler отдаёт /metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RegisterPool публикует статистику пула pgx (pgxpool_*).
func (m *Metrics) RegisterPool(pool *pgxpool.Pool) {
	m.registry.MustRegister(&poolCollector{pool: pool,
		total:    prometheus.NewDesc("pgxpool_total_conns", "Всего соединений в пуле.", nil, nil),
		idle:     prometheus.NewDesc("pgxpool_idle_conns", "Простаивающих соединений.", nil, nil),
		acquired: prometheus.NewDesc("pgxpool_acquired_conns", "Занятых соединений.", nil, nil),
		max:      prometheus.NewDesc("pgxpool_max_conns", "Лимит соединений пула.", nil, nil),
		waiting:  prometheus.NewDesc("pgxpool_empty_acquire_total", "Сколько раз ждали свободного соединения.", nil, nil),
	})
}

type poolCollector struct {
	pool                                *pgxpool.Pool
	total, idle, acquired, max, waiting *prometheus.Desc
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.total
	ch <- c.idle
	ch <- c.acquired
	ch <- c.max
	ch <- c.waiting
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.pool.Stat()
	ch <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(s.TotalConns()))
	ch <- prometheus.MustNewConstMetric(c.idle, prometheus.GaugeValue, float64(s.IdleConns()))
	ch <- prometheus.MustNewConstMetric(c.acquired, prometheus.GaugeValue, float64(s.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(c.max, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.waiting, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
}
