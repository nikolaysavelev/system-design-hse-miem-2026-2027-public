package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewTicket_CodeUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		tk := NewTicket(uuid.New(), uuid.New(), int64(i), time.Now())
		if len(tk.Code) != 16 {
			t.Fatalf("код должен быть 16 символов, получено %q", tk.Code)
		}
		if seen[tk.Code] {
			t.Fatalf("повтор кода %q", tk.Code)
		}
		seen[tk.Code] = true
	}
}
