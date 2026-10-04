package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikolaysavelev/soldout/internal/notification/api"
)

func newGateway(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(statuses) {
			i = len(statuses) - 1
		}
		w.WriteHeader(statuses[i])
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func note() api.Notification {
	return api.Notification{ID: uuid.New(), UserID: uuid.New(), Kind: "tickets_issued", Payload: map[string]any{"order_id": "x"}}
}

func TestSendRetriesOnceOn5xx(t *testing.T) {
	srv, calls := newGateway(t, http.StatusBadGateway, http.StatusAccepted)
	if err := New(Options{URL: srv.URL, Retries: 1}).Send(context.Background(), note()); err != nil {
		t.Fatalf("ожидали успех после повтора, получили %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("ожидали 2 вызова, было %d", calls.Load())
	}
}

func TestSendGivesUpAfterRetries(t *testing.T) {
	srv, calls := newGateway(t, http.StatusBadGateway)
	err := New(Options{URL: srv.URL, Retries: 1}).Send(context.Background(), note())
	var up *ErrUpstream
	if !errors.As(err, &up) || up.Status != http.StatusBadGateway {
		t.Fatalf("ожидали ErrUpstream 502, получили %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("ожидали 2 вызова, было %d", calls.Load())
	}
}

func TestSendNoRetryOn4xx(t *testing.T) {
	srv, calls := newGateway(t, http.StatusBadRequest)
	if err := New(Options{URL: srv.URL, Retries: 1}).Send(context.Background(), note()); err == nil {
		t.Fatal("ожидали ошибку на 400")
	}
	if calls.Load() != 1 {
		t.Fatalf("на 4xx повтора быть не должно, было %d вызовов", calls.Load())
	}
}

func TestSendTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer srv.Close()
	start := time.Now()
	err := New(Options{URL: srv.URL, Timeout: 50 * time.Millisecond, Retries: 1}).Send(context.Background(), note())
	if err == nil {
		t.Fatal("ожидали ошибку таймаута")
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("таймаут клиента не сработал: %v", time.Since(start))
	}
}
