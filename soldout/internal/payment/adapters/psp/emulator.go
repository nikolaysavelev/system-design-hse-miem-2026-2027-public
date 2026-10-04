// Package psp — эмулятор платёжного провайдера с управляемыми отказами и задержкой (fault-flags).
package psp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/payment/domain"
)

// Emulator — in-process PSP.
type Emulator struct {
	failRate float64
	delay    time.Duration
	rnd      func() float64
}

// New создаёт эмулятор: failRate ∈ [0,1] — доля отказов, delay — задержка ответа.
func New(failRate float64, delay time.Duration) *Emulator {
	return &Emulator{failRate: failRate, delay: delay, rnd: mrand.Float64}
}

// Charge — «списание»: ждёт delay (уважая ctx), затем отказывает с вероятностью failRate.
func (e *Emulator) Charge(ctx context.Context, _ uuid.UUID, amountMinor int64) (domain.PSPResult, error) {
	if e.delay > 0 {
		select {
		case <-ctx.Done():
			return domain.PSPResult{}, ctx.Err()
		case <-time.After(e.delay):
		}
	}
	if amountMinor <= 0 {
		return domain.PSPResult{Succeeded: false, Reason: "invalid_amount"}, nil
	}
	if e.failRate > 0 && e.rnd() < e.failRate {
		return domain.PSPResult{Succeeded: false, Reason: "psp_declined"}, nil
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	return domain.PSPResult{Succeeded: true, Ref: "psp-" + hex.EncodeToString(b[:])}, nil
}
