package domain

import "testing"

func TestPricing_PriceFor(t *testing.T) {
	p := Pricing{BasePriceMinor: 500000, StepSeats: 200, StepPct: 5}
	cases := map[int]int64{
		1:   500000,
		200: 500000, // последнее место первой ступени
		201: 525000, // первое место второй ступени
		400: 525000,
		401: 551250,
		601: 578812, // 578812.5 → вниз до копейки
		801: 607753, // 607753.125
	}
	for ordinal, want := range cases {
		if got := p.PriceFor(ordinal); got != want {
			t.Errorf("PriceFor(%d) = %d, ожидалось %d", ordinal, got, want)
		}
	}
}

func TestPricing_RoundsDownOnce(t *testing.T) {
	// округление один раз в конце, а не на каждой ступени: 999 * 1.05^2 = 1101.4475 → 1101
	p := Pricing{BasePriceMinor: 999, StepSeats: 1, StepPct: 5}
	if got := p.PriceFor(3); got != 1101 {
		t.Fatalf("PriceFor(3) = %d, ожидалось 1101", got)
	}
}

func TestPricing_ZeroPctAndBounds(t *testing.T) {
	flat := Pricing{BasePriceMinor: 100, StepSeats: 10, StepPct: 0}
	if got := flat.PriceFor(1000); got != 100 {
		t.Fatalf("без роста цена постоянна: %d", got)
	}
	p := Pricing{BasePriceMinor: 100, StepSeats: 10, StepPct: 5}
	if p.PriceFor(0) != p.PriceFor(1) {
		t.Fatal("ordinal < 1 считается первым местом")
	}
	if got := p.PriceFor(1000); got <= 100 {
		t.Fatalf("цена должна расти: %d", got)
	}
}

func TestPricing_Validate(t *testing.T) {
	if err := (Pricing{BasePriceMinor: 1, StepSeats: 1}).Validate(); err != nil {
		t.Fatalf("валидная политика: %v", err)
	}
	for _, p := range []Pricing{{BasePriceMinor: 0, StepSeats: 1}, {BasePriceMinor: 1, StepSeats: 0}, {BasePriceMinor: 1, StepSeats: 1, StepPct: -1}} {
		if p.Validate() == nil {
			t.Errorf("политика %+v должна быть невалидной", p)
		}
	}
}
