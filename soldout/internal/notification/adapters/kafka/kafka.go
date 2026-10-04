// Package kafka — источник событий notifier из Kafka (franz-go) и публикация в DLQ.
//
// Consumer group notifier, at-least-once. Сообщения обрабатываются параллельно (письмо идёт ~1 с, последовательно
// поток бы не успевал), а смещение партиции фиксируется по «водяной отметке»: до первого ещё не обработанного
// сообщения. Медленное письмо или повторы одного сообщения не задерживают остальные, но и не дают закоммитить
// то, что не обработано: после падения придут повторы (их гасит идемпотентность), потерь нет.
// Порядок гарантирован только внутри ключа (order_id) — и только до параллельной обработки (ADR-003).
//
// Ребаланс (занятие 5: реплики и автоскейлинг по лагу). Когда группа забирает партицию, потребитель перестаёт
// начинать её сообщения, дожидается только уже начатых и коммитит отметку: партиция переходит к новой реплике
// за секунды. Ждать все начатые сообщения всех партиций нельзя: цикл опроса продолжает начинать новые, ребаланс
// не завершается, и новая реплика простаивает до конца очереди.
package kafka

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/nikolaysavelev/soldout/internal/notification/app"
)

// Options — настройки потребителя.
type Options struct {
	Brokers     []string
	Topic       string // outbox.event.order
	DLQTopic    string // outbox.event.order.dlq
	Group       string // notifier
	Concurrency int    // одновременно обрабатываемых сообщений (backpressure: опрос ждёт свободный слот)
}

// Consumer — источник событий.
type Consumer struct {
	cl       *kgo.Client
	adm      *kadm.Client
	opts     Options
	logger   *slog.Logger
	tracker  *tracker
	gate     *gate
	inflight sync.WaitGroup
}

// Сколько ждём начатые сообщения: при отзыве партиции и при остановке процесса (меньше terminationGracePeriod пода).
const (
	revokeWait   = 15 * time.Second
	shutdownWait = 15 * time.Second
)

// New подключается к Kafka.
func New(opts Options, logger *slog.Logger) (*Consumer, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 256
	}
	c := &Consumer{opts: opts, logger: logger, tracker: newTracker(), gate: newGate()}
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(opts.Brokers...),
		kgo.ConsumerGroup(opts.Group),
		kgo.ConsumeTopics(opts.Topic),
		kgo.AutoCommitMarks(), // коммитятся только отмеченные смещения (водяная отметка), раз в секунду
		kgo.AutoCommitInterval(time.Second),
		kgo.FetchMaxWait(100*time.Millisecond),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.ClientID("notifier"),
		// перед отдачей партиций: новых сообщений этих партиций не начинаем, начатые дожидаемся, отметку коммитим
		kgo.OnPartitionsRevoked(func(ctx context.Context, cl *kgo.Client, revoked map[string][]int32) {
			parts := revoked[opts.Topic]
			c.gate.close(parts)
			waitCtx, cancel := context.WithTimeout(ctx, revokeWait)
			defer cancel()
			if !c.gate.wait(waitCtx, parts) {
				logger.Warn("kafka: отзыв партиций, начатые сообщения не завершились за отведённое время", "partitions", parts)
			}
			if err := cl.CommitMarkedOffsets(ctx); err != nil {
				logger.Warn("kafka: commit при отзыве партиций", "err", err)
			}
			c.tracker.reset(parts)
			logger.Info("kafka: партиции отданы", "partitions", parts)
		}),
		kgo.OnPartitionsLost(func(_ context.Context, _ *kgo.Client, lost map[string][]int32) {
			c.gate.close(lost[opts.Topic])
			c.tracker.reset(lost[opts.Topic])
		}),
		kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, assigned map[string][]int32) {
			c.gate.open(assigned[opts.Topic])
			logger.Info("kafka: партиции получены", "partitions", assigned[opts.Topic])
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("kafka: клиент: %w", err)
	}
	c.cl, c.adm = cl, kadm.NewClient(cl)
	return c, nil
}

// Close — фиксирует отмеченные смещения и закрывает клиента: участник выходит из группы сам (LeaveGroup),
// партиции переходят к остальным репликам сразу, а не через session timeout.
func (c *Consumer) Close() {
	c.inflight.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.cl.CommitMarkedOffsets(ctx)
	c.cl.Close()
}

// Ping — проверка соединения с брокером (readyz).
func (c *Consumer) Ping(ctx context.Context) error { return c.cl.Ping(ctx) }

// Run — цикл опроса до отмены ctx. Начатые сообщения при остановке дорабатывают (до shutdownWait): обработчик
// получает контекст, который отмена ctx не трогает, иначе SIGTERM обрывал бы письма на полпути и они уходили бы повторно.
func (c *Consumer) Run(ctx context.Context, handle func(context.Context, app.Message) error) error {
	sem := make(chan struct{}, c.opts.Concurrency)
	workCtx, cancelWork := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelWork()
	drain := func() {
		done := make(chan struct{})
		go func() { c.inflight.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(shutdownWait):
			c.logger.Warn("kafka: остановка, начатые сообщения прерваны", "timeout", shutdownWait.String())
			cancelWork()
			<-done
		}
	}
	for {
		fetches := c.cl.PollRecords(ctx, c.opts.Concurrency)
		if ctx.Err() != nil {
			drain()
			return nil
		}
		fetches.EachError(func(t string, p int32, err error) {
			c.logger.WarnContext(ctx, "kafka: ошибка опроса", "topic", t, "partition", p, "err", err)
		})
		for _, r := range fetches.Records() {
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				drain()
				return nil
			}
			if !c.gate.enter(r.Partition) { // партицию забирает группа: сообщение прочитает новый владелец
				<-sem
				continue
			}
			c.tracker.start(r)
			c.inflight.Add(1)
			go func(r *kgo.Record) {
				defer func() { <-sem; c.gate.leave(r.Partition); c.inflight.Done() }()
				ctx := workCtx
				if err := handle(ctx, toMessage(r)); err != nil {
					// не обработано и не в DLQ: отметка партиции не пройдёт это смещение — лаг растёт, это видно
					c.logger.ErrorContext(ctx, "kafka: сообщение не обработано, смещение не фиксируется", "partition", r.Partition, "offset", r.Offset, "err", err)
					return
				}
				if next, ok := c.tracker.finish(r); ok {
					c.cl.MarkCommitOffsets(map[string]map[int32]kgo.EpochOffset{
						r.Topic: {r.Partition: {Epoch: r.LeaderEpoch, Offset: next}},
					})
				}
			}(r)
		}
	}
}

// tracker считает водяную отметку по партициям: смещение, до которого всё обработано.
type tracker struct {
	mu       sync.Mutex
	inflight map[int32]map[int64]struct{}
	maxDone  map[int32]int64
}

func newTracker() *tracker {
	return &tracker{inflight: map[int32]map[int64]struct{}{}, maxDone: map[int32]int64{}}
}

func (t *tracker) start(r *kgo.Record) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight[r.Partition] == nil {
		t.inflight[r.Partition] = map[int64]struct{}{}
	}
	t.inflight[r.Partition][r.Offset] = struct{}{}
}

// finish отмечает сообщение обработанным и возвращает новую отметку (следующее смещение к коммиту).
func (t *tracker) finish(r *kgo.Record) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.inflight[r.Partition], r.Offset)
	if r.Offset > t.maxDone[r.Partition] {
		t.maxDone[r.Partition] = r.Offset
	}
	low := int64(-1)
	for off := range t.inflight[r.Partition] {
		if low == -1 || off < low {
			low = off
		}
	}
	if low == -1 {
		return t.maxDone[r.Partition] + 1, true // всё начатое обработано
	}
	return low, true // коммит до первого незавершённого
}

// reset забывает партиции: после возврата партиции отсчёт начинается с зафиксированного смещения.
func (t *tracker) reset(parts []int32) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, p := range parts {
		delete(t.inflight, p)
		delete(t.maxDone, p)
	}
}

// gate — пропуск сообщений по партициям: закрытая партиция новых сообщений не начинает, wait ждёт начатые.
type gate struct {
	mu      sync.Mutex
	closed  map[int32]bool
	running map[int32]int
}

func newGate() *gate { return &gate{closed: map[int32]bool{}, running: map[int32]int{}} }

// enter — можно ли начать сообщение партиции; true учитывает его как начатое.
func (g *gate) enter(p int32) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed[p] {
		return false
	}
	g.running[p]++
	return true
}

func (g *gate) leave(p int32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.running[p]--
}

func (g *gate) close(parts []int32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range parts {
		g.closed[p] = true
	}
}

func (g *gate) open(parts []int32) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range parts {
		delete(g.closed, p)
	}
}

// wait ждёт, пока начатые сообщения партиций завершатся; false — контекст истёк раньше.
func (g *gate) wait(ctx context.Context, parts []int32) bool {
	t := time.NewTicker(20 * time.Millisecond)
	defer t.Stop()
	for {
		g.mu.Lock()
		busy := 0
		for _, p := range parts {
			busy += g.running[p]
		}
		g.mu.Unlock()
		if busy == 0 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
		}
	}
}

func toMessage(r *kgo.Record) app.Message {
	h := make(map[string]string, len(r.Headers))
	for _, x := range r.Headers {
		h[x.Key] = string(x.Value)
	}
	return app.Message{Topic: r.Topic, Partition: r.Partition, Offset: r.Offset, Key: string(r.Key), Value: r.Value, Headers: h, Timestamp: r.Timestamp}
}

// Publish — app.DLQ: исходное сообщение с заголовками error, attempts, original_offset в топик DLQ.
func (c *Consumer) Publish(ctx context.Context, m app.Message, cause error, attempts int) error {
	rec := &kgo.Record{Topic: c.opts.DLQTopic, Key: []byte(m.Key), Value: m.Value}
	for k, v := range app.DLQHeaders(m, cause, attempts) {
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: k, Value: []byte(v)})
	}
	return c.cl.ProduceSync(ctx, rec).FirstErr()
}

// Lag — отставание группы по топику (сообщений): сумма по партициям.
func (c *Consumer) Lag(ctx context.Context) (int64, error) {
	return groupLag(ctx, c.adm, c.opts.Group, c.opts.Topic)
}

// LagReader читает лаг группы без участия в ней: экспортёр лага работает и тогда, когда потребитель лежит
// (make kill-notifier: лаг растёт на дашборде, пока notifier остановлен).
type LagReader struct {
	cl           *kgo.Client
	adm          *kadm.Client
	group, topic string
}

// NewLagReader подключается к Kafka как административный клиент.
func NewLagReader(brokers []string, group, topic string) (*LagReader, error) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.ClientID("notifier-lag"))
	if err != nil {
		return nil, fmt.Errorf("kafka: клиент: %w", err)
	}
	return &LagReader{cl: cl, adm: kadm.NewClient(cl), group: group, topic: topic}, nil
}

// Lag — отставание группы по топику.
func (l *LagReader) Lag(ctx context.Context) (int64, error) {
	return groupLag(ctx, l.adm, l.group, l.topic)
}

// Ping — проверка соединения с брокером.
func (l *LagReader) Ping(ctx context.Context) error { return l.cl.Ping(ctx) }

// Close закрывает клиента.
func (l *LagReader) Close() { l.cl.Close() }

func groupLag(ctx context.Context, adm *kadm.Client, group, topic string) (int64, error) {
	lags, err := adm.Lag(ctx, group)
	if err != nil {
		return 0, err
	}
	l, ok := lags[group]
	if !ok {
		return 0, nil
	}
	if err := l.Error(); err != nil {
		return 0, err
	}
	return l.Lag.TotalByTopic()[topic].Lag, nil
}
