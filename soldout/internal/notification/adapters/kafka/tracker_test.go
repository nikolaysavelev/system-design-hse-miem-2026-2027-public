package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Водяная отметка не проходит незавершённое сообщение: коммит до первого «висящего» смещения.
func TestTrackerWatermark(t *testing.T) {
	tr := newTracker()
	rec := func(off int64) *kgo.Record { return &kgo.Record{Partition: 0, Offset: off} }
	for off := int64(10); off <= 13; off++ {
		tr.start(rec(off))
	}
	steps := []struct {
		done int64
		want int64
	}{
		{11, 10}, // 10 ещё идёт (медленное письмо) — коммитить можно только до 10
		{13, 10},
		{10, 12}, // 10 и 11 готовы, 12 идёт
		{12, 14}, // всё готово — следующее смещение 14
	}
	for _, s := range steps {
		got, _ := tr.finish(rec(s.done))
		if got != s.want {
			t.Fatalf("после %d: отметка %d, ожидали %d", s.done, got, s.want)
		}
	}
}

// Отзыв партиции: новые сообщения этой партиции не начинаются, ожидание смотрит только на неё.
func TestGateRevokeWaitsOnlyRevokedPartitions(t *testing.T) {
	g := newGate()
	if !g.enter(0) || !g.enter(1) {
		t.Fatal("открытые партиции должны пропускать сообщения")
	}
	g.close([]int32{0})
	if g.enter(0) {
		t.Fatal("закрытая партиция не должна начинать новые сообщения")
	}
	if !g.enter(1) {
		t.Fatal("отзыв партиции 0 не должен останавливать партицию 1")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if g.wait(ctx, []int32{0}) {
		t.Fatal("wait вернулся, хотя сообщение партиции 0 ещё в работе")
	}

	go func() { time.Sleep(30 * time.Millisecond); g.leave(0) }()
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if !g.wait(ctx2, []int32{0}) { // партиция 1 занята двумя сообщениями, ждать её не нужно
		t.Fatal("wait должен дождаться только партиции 0")
	}

	g.open([]int32{0})
	if !g.enter(0) {
		t.Fatal("после возврата партиции сообщения снова принимаются")
	}
}

// После отзыва отметка партиции начинается заново: старое maxDone не должно уводить коммит вперёд.
func TestTrackerResetForgetsPartition(t *testing.T) {
	tr := newTracker()
	r := &kgo.Record{Partition: 0, Offset: 41}
	tr.start(r)
	if next, _ := tr.finish(r); next != 42 {
		t.Fatalf("next = %d, ожидалось 42", next)
	}
	tr.reset([]int32{0})
	r = &kgo.Record{Partition: 0, Offset: 10}
	tr.start(r)
	if next, _ := tr.finish(r); next != 11 {
		t.Fatalf("после reset next = %d, ожидалось 11", next)
	}
}
