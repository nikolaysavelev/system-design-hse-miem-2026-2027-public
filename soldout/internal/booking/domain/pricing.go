package domain

import (
	"fmt"
	"math/big"

	"github.com/nikolaysavelev/soldout/internal/booking/api"
)

// Pricing — ценовая политика сектора (HW1, V2): цена растет на StepPct процентов каждые StepSeats мест.
type Pricing struct {
	BasePriceMinor int64
	StepSeats      int
	StepPct        int
}

// Validate — параметры политики осмысленны.
func (p Pricing) Validate() error {
	if p.BasePriceMinor <= 0 || p.StepSeats <= 0 || p.StepPct < 0 {
		return fmt.Errorf("%w: ценовая политика base=%d step=%d pct=%d", api.ErrValidation, p.BasePriceMinor, p.StepSeats, p.StepPct)
	}
	return nil
}

// PriceFor — цена места с порядковым номером ordinal (с 1): base * (1 + pct/100)^floor((ordinal-1)/step),
// округление вниз до копейки. Считается точно в целых числах: base * (100+pct)^s / 100^s.
func (p Pricing) PriceFor(ordinal int) int64 {
	if ordinal < 1 {
		ordinal = 1
	}
	steps := int64((ordinal - 1) / p.StepSeats)
	num := new(big.Int).Exp(big.NewInt(int64(100+p.StepPct)), big.NewInt(steps), nil)
	num.Mul(num, big.NewInt(p.BasePriceMinor))
	den := new(big.Int).Exp(big.NewInt(100), big.NewInt(steps), nil)
	return num.Quo(num, den).Int64()
}
