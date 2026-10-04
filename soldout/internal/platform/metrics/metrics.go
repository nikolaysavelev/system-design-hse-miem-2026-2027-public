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

	HTTPRequests       *prometheus.CounterVec
	HTTPDuration       *prometheus.HistogramVec
	DBQueryDuration    *prometheus.HistogramVec
	DBAcquireWait      prometheus.Histogram
	DBAcquireTimeouts  prometheus.Counter
	HoldsActive        prometheus.Gauge
	ExpirerBatchSize   prometheus.Histogram
	HoldContention     *prometheus.CounterVec
	CacheOps           *prometheus.CounterVec
	EventbusPublished  *prometheus.CounterVec
	EventbusDropped    prometheus.Counter
	EventbusQueueDepth prometheus.Gauge
	QueueSize          *prometheus.GaugeVec
	QueueInflight      *prometheus.GaugeVec
	QueueAdmitted      prometheus.Counter
	QueueAdmissionRate prometheus.Gauge
}

// Явные бакеты 5 мс … 5 с: дефолтные заканчиваются на 10 с и слепы на хвостах под штормом.
var latencyBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

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
			Buckets: latencyBuckets,
		}, []string{"method", "route", "status"}),
		DBQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Длительность SQL-запросов по имени запроса (комментарий /* name */ в начале SQL).",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}, []string{"query"}),
		DBAcquireWait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "db_pool_acquire_wait_seconds",
			Help:    "Ожидание свободного соединения в пуле pgx (закон Литтла: очередь переехала из БД в приложение).",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2, 5},
		}),
		DBAcquireTimeouts: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "db_pool_acquire_timeouts_total",
			Help: "Сколько раз не дождались соединения (→ 503 db_busy).",
		}),
		HoldsActive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "holds_active",
			Help: "Число активных hold (обновляется expirer'ом).",
		}),
		ExpirerBatchSize: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "expirer_batch_size",
			Help:    "Размер батча expirer (строк за один UPDATE … SKIP LOCKED).",
			Buckets: []float64{1, 10, 50, 100, 250, 500},
		}),
		HoldContention: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hold_contention_total",
			Help: "Исходы попыток удержать место: created, seat_held, seat_sold, limit, sector_exhausted, idempotent_replay.",
		}, []string{"outcome"}),
		CacheOps: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_ops_total",
			Help: "Операции кэша карты зала: hit, miss, stale_write_skipped, invalidate, error.",
		}, []string{"op"}),
		EventbusPublished: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "eventbus_published_total",
			Help: "События, опубликованные в in-process шину, по топику.",
		}, []string{"topic"}),
		EventbusDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "eventbus_dropped_total",
			Help: "События, отброшенные из-за переполнения буфера шины (проекция догонит по TTL/backfill).",
		}),
		EventbusQueueDepth: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "eventbus_queue_depth",
			Help: "Текущая глубина буфера шины событий.",
		}),
		QueueSize: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "queue_size",
			Help: "Размер waiting room (ожидающих допуска) по мероприятию.",
		}, []string{"event"}),
		QueueInflight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "queue_inflight",
			Help: "Пользователи между ZPOPMIN и выдачей допуска.",
		}, []string{"event"}),
		QueueAdmitted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_admitted_total",
			Help: "Выдано допусков admitter'ом.",
		}),
		QueueAdmissionRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "queue_admission_rate",
			Help: "ADMISSION_RATE — допусков в секунду (ручка).",
		}),
	}
	reg.MustRegister(
		collectors.NewGoCollector(collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsGC, collectors.MetricsMemory, collectors.MetricsScheduler)),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.HTTPRequests, m.HTTPDuration, m.DBQueryDuration, m.DBAcquireWait, m.DBAcquireTimeouts, m.HoldsActive,
		m.ExpirerBatchSize, m.HoldContention, m.CacheOps, m.EventbusPublished, m.EventbusDropped, m.EventbusQueueDepth,
		m.QueueSize, m.QueueInflight, m.QueueAdmitted, m.QueueAdmissionRate,
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
