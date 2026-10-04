// Package http — доставка уведомлений через внешний почтовый шлюз (эмулятор cmd/mailgw).
//
// Так выглядит реальный провайдер: медленно и иногда 5xx. Таймаут клиента 2 с, один повтор при 5xx.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nikolaysavelev/soldout/internal/notification/api"
)

// Options — настройки отправителя.
type Options struct {
	URL       string        // базовый адрес шлюза, например http://mailgw:8090
	Timeout   time.Duration // таймаут одного вызова (по умолчанию 2 с)
	Retries   int           // повторов при 5xx (по умолчанию 1)
	Transport http.RoundTripper
}

// Sender — реализация app.Sender.
type Sender struct {
	url     string
	retries int
	client  *http.Client
}

// New создаёт отправитель.
func New(opts Options) *Sender {
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	return &Sender{url: opts.URL + "/send", retries: opts.Retries, client: &http.Client{Timeout: opts.Timeout, Transport: opts.Transport}}
}

// ErrUpstream — шлюз ответил 5xx после всех повторов.
type ErrUpstream struct{ Status int }

func (e *ErrUpstream) Error() string {
	return fmt.Sprintf("notification/http: почтовый шлюз ответил %d", e.Status)
}

type message struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Send — отправка письма; повтор только при 5xx (4xx — ошибка запроса, повтор не поможет).
func (s *Sender) Send(ctx context.Context, n api.Notification) error {
	body, err := json.Marshal(message{
		To:      n.UserID.String() + "@users.soldout.local",
		Subject: subject(n.Kind),
		Body:    fmt.Sprintf("%v", n.Payload),
	})
	if err != nil {
		return fmt.Errorf("notification/http: marshal: %w", err)
	}
	var last error
	for attempt := 0; attempt <= s.retries; attempt++ {
		status, err := s.post(ctx, body)
		switch {
		case err != nil:
			last = err
		case status >= 500:
			last = &ErrUpstream{Status: status}
		case status >= 400:
			return fmt.Errorf("notification/http: шлюз отклонил письмо: %d", status)
		default:
			return nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return last
}

func (s *Sender) post(ctx context.Context, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("notification/http: запрос: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("notification/http: вызов шлюза: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func subject(kind string) string {
	if kind == "tickets_issued" {
		return "Ваши билеты"
	}
	return "Уведомление soldout"
}
