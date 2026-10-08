//go:build semgrep_red

// Красный пример для make fitness-red-semgrep: «первый рефлекс» акта 3 — письмо после commit.
// Файл не компилируется в обычной сборке (build tag), его читает только semgrep.
package red

import (
	"context"
	"net/http"
)

type store interface {
	InTx(ctx context.Context, fn func() error) error
}

type mailer interface {
	Send(ctx context.Context, to string) error
}

func payThenMail(ctx context.Context, s store, m mailer, client *http.Client, req *http.Request) error {
	err := s.InTx(ctx, func() error { return nil }) // orders.paid
	if err != nil {
		return err
	}
	_ = m.Send(ctx, "user@example.com") // падение процесса здесь теряет письмо
	go func() {
		_, _ = client.Do(req) // горутина умирает вместе с процессом
	}()
	return nil
}
