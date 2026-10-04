// Package faults — флаги управляемых сбоев (fault injection) из окружения.
//
// На занятии 1 реализован механизм и флаги платёжного эмулятора; CRASH_* используются с занятия 3.
package faults

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Flags — включённые сбои.
type Flags struct {
	PSPFailRate      float64       // FAULT_PSP_FAIL_RATE (алиас PSP_FAIL_RATE): доля отказов эмулятора, 0..1
	SlowPSP          time.Duration // FAULT_SLOW_PSP_MS (алиас PSP_DELAY_MS): задержка ответа эмулятора
	CrashAfterCommit string        // FAULT_CRASH_AFTER_COMMIT=order.paid — os.Exit после commit в указанной точке (L3)
	CrashBeforeAck   string        // FAULT_CRASH_BEFORE_ACK=ticketing — падение потребителя до ack (L3)
}

// FromEnv читает флаги. Пустое значение = сбой выключен.
func FromEnv() (Flags, error) {
	f := Flags{
		CrashAfterCommit: os.Getenv("FAULT_CRASH_AFTER_COMMIT"),
		CrashBeforeAck:   os.Getenv("FAULT_CRASH_BEFORE_ACK"),
	}
	rate := firstNonEmpty(os.Getenv("FAULT_PSP_FAIL_RATE"), os.Getenv("PSP_FAIL_RATE"))
	if rate != "" {
		v, err := strconv.ParseFloat(rate, 64)
		if err != nil || v < 0 || v > 1 {
			return f, fmt.Errorf("faults: PSP_FAIL_RATE=%q: ожидается число 0..1", rate)
		}
		f.PSPFailRate = v
	}
	delay := firstNonEmpty(os.Getenv("FAULT_SLOW_PSP_MS"), os.Getenv("PSP_DELAY_MS"))
	if delay != "" {
		ms, err := strconv.Atoi(delay)
		if err != nil || ms < 0 {
			return f, fmt.Errorf("faults: PSP_DELAY_MS=%q: ожидается число миллисекунд", delay)
		}
		f.SlowPSP = time.Duration(ms) * time.Millisecond
	}
	return f, nil
}

// MaybeCrashAfterCommit завершает процесс, если включён сбой в точке point. Вызывается сразу после commit.
func (f Flags) MaybeCrashAfterCommit(logger *slog.Logger, point string) {
	if f.CrashAfterCommit != "" && f.CrashAfterCommit == point {
		logger.Error("FAULT_CRASH_AFTER_COMMIT: аварийное завершение", "point", point)
		os.Exit(3)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
