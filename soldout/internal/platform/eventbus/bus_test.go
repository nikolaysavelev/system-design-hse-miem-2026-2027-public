package eventbus

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type testEvent struct{ key, val string }

func (testEvent) Topic() string { return "t" }
func (e testEvent) Key() string { return e.key }

func TestBus_OrderPerKeyAndFanout(t *testing.T) {
	bus := New(4, 100, nil, slog.Default())
	var mu sync.Mutex
	got := map[string][]string{}
	bus.Subscribe("t", func(_ context.Context, e Event) error {
		te := e.(testEvent)
		mu.Lock()
		got[te.key] = append(got[te.key], te.val)
		mu.Unlock()
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { bus.Run(ctx); close(done) }()

	for i := 0; i < 50; i++ {
		for _, k := range []string{"a", "b", "c"} {
			_ = bus.Publish(ctx, testEvent{key: k, val: string(rune('0' + i%10))})
		}
	}
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()
	for _, k := range []string{"a", "b", "c"} {
		if len(got[k]) != 50 {
			t.Fatalf("ключ %s: ожидалось 50 событий, получено %d", k, len(got[k]))
		}
		for i, v := range got[k] {
			if v != string(rune('0'+i%10)) {
				t.Fatalf("ключ %s: порядок нарушен на позиции %d: %s", k, i, v)
			}
		}
	}
}

func TestBus_DropOnOverflow(t *testing.T) {
	bus := New(1, 2, nil, slog.Default())
	ctx := context.Background()
	for i := 0; i < 5; i++ { // воркеры не запущены — буфер 2, остальное отброшено, Publish не блокирует
		if err := bus.Publish(ctx, testEvent{key: "k"}); err != nil {
			t.Fatal(err)
		}
	}
	if d := bus.depth(); d != 2 {
		t.Fatalf("ожидалась глубина 2, получено %d", d)
	}
}
