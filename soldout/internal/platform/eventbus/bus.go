// Package eventbus — in-process шина доменных событий: шов между модулями и заготовка для outbox (занятие 3).
//
// Издатель (booking) публикует события после commit транзакции; подписчик (catalog) обновляет проекцию карты зала.
// События с одним Key() обрабатываются одним воркером — порядок «held → free» на одно место сохраняется.
// Переполнение буфера не блокирует издателя: событие отбрасывается (метрика eventbus_dropped_total), проекция
// догоняет по TTL кэша и backfill'у. На занятии 3 Publisher становится INSERT INTO outbox в той же транзакции.
package eventbus

import (
	"context"
	"hash/fnv"
	"log/slog"
	"sync"
	"time"

	"github.com/nikolaysavelev/soldout/internal/platform/metrics"
)

// Event — доменное событие.
type Event interface {
	Topic() string
	Key() string // ключ упорядочивания (например, event_id:seat_id)
}

// Handler обрабатывает событие; ошибка логируется, повторов нет (at-most-once внутри процесса).
type Handler func(ctx context.Context, e Event) error

// Publisher — порт издателя.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// Bus — шина с N воркерами и буфером на воркер.
type Bus struct {
	mu      sync.RWMutex
	subs    map[string][]Handler
	queues  []chan Event
	wg      sync.WaitGroup
	m       *metrics.Metrics
	logger  *slog.Logger
	timeout time.Duration
	started bool
}

// New создаёт шину: workers воркеров, buffer событий в очереди каждого.
func New(workers, buffer int, m *metrics.Metrics, logger *slog.Logger) *Bus {
	if workers <= 0 {
		workers = 4
	}
	if buffer <= 0 {
		buffer = 1024
	}
	b := &Bus{subs: map[string][]Handler{}, queues: make([]chan Event, workers), m: m, logger: logger, timeout: 5 * time.Second}
	for i := range b.queues {
		b.queues[i] = make(chan Event, buffer)
	}
	return b
}

// Subscribe регистрирует обработчик топика. Вызывается при сборке (cmd), до Run.
func (b *Bus) Subscribe(topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], h)
}

// Publish кладёт событие в очередь воркера по Key(). Не блокирует: при переполнении событие отбрасывается.
func (b *Bus) Publish(_ context.Context, e Event) error {
	q := b.queues[b.shard(e.Key())]
	select {
	case q <- e:
		if b.m != nil {
			b.m.EventbusPublished.WithLabelValues(e.Topic()).Inc()
			b.m.EventbusQueueDepth.Set(float64(b.depth()))
		}
	default:
		if b.m != nil {
			b.m.EventbusDropped.Inc()
		}
		b.logger.Warn("eventbus: буфер переполнен, событие отброшено", "topic", e.Topic(), "key", e.Key())
	}
	return nil
}

// Run запускает воркеры и блокируется до отмены ctx; после отмены очереди дочитываются (drain).
func (b *Bus) Run(ctx context.Context) {
	b.mu.Lock()
	b.started = true
	b.mu.Unlock()
	for i := range b.queues {
		b.wg.Add(1)
		go b.worker(ctx, b.queues[i])
	}
	<-ctx.Done()
	b.wg.Wait()
}

func (b *Bus) worker(ctx context.Context, q chan Event) {
	defer b.wg.Done()
	for {
		select {
		case e := <-q:
			b.dispatch(e)
		case <-ctx.Done():
			for { // drain
				select {
				case e := <-q:
					b.dispatch(e)
				default:
					return
				}
			}
		}
	}
}

func (b *Bus) dispatch(e Event) {
	b.mu.RLock()
	handlers := b.subs[e.Topic()]
	b.mu.RUnlock()
	for _, h := range handlers {
		ctx, cancel := context.WithTimeout(context.Background(), b.timeout)
		if err := h(ctx, e); err != nil {
			b.logger.Error("eventbus: обработчик вернул ошибку", "topic", e.Topic(), "key", e.Key(), "err", err)
		}
		cancel()
	}
	if b.m != nil {
		b.m.EventbusQueueDepth.Set(float64(b.depth()))
	}
}

func (b *Bus) shard(key string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return int(h.Sum32() % uint32(len(b.queues)))
}

func (b *Bus) depth() int {
	n := 0
	for _, q := range b.queues {
		n += len(q)
	}
	return n
}

// Nop — издатель-заглушка (тесты, ветки без подписчиков).
type Nop struct{}

// Publish ничего не делает.
func (Nop) Publish(context.Context, Event) error { return nil }
