package psp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEmulator_FailRate(t *testing.T) {
	ctx := context.Background()
	always := New(1, 0)
	res, err := always.Charge(ctx, uuid.New(), 100)
	if err != nil || res.Succeeded || res.Reason != "psp_declined" {
		t.Fatalf("failRate=1 должен отказывать: %+v, %v", res, err)
	}
	never := New(0, 0)
	res, err = never.Charge(ctx, uuid.New(), 100)
	if err != nil || !res.Succeeded || res.Ref == "" {
		t.Fatalf("failRate=0 должен списывать: %+v, %v", res, err)
	}
	if res, _ := never.Charge(ctx, uuid.New(), 0); res.Succeeded {
		t.Fatal("нулевая сумма должна отклоняться")
	}
}

func TestEmulator_DelayRespectsContext(t *testing.T) {
	slow := New(0, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := slow.Charge(ctx, uuid.New(), 100)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ожидался DeadlineExceeded, получено %v", err)
	}
}
