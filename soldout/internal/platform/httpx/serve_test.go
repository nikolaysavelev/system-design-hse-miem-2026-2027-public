package httpx

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Остановка под нагрузкой: в момент сигнала в работе 40 запросов. Все обязаны получить 200,
// ни одно соединение не обрывается; после остановки порт закрыт. Fitness function выкатки без простоя (ADR-004).
func TestServeUntil_InFlightRequestsComplete(t *testing.T) {
	const inFlight = 40
	var started atomic.Int32
	allStarted := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if started.Add(1) == inFlight {
			close(allStarted)
		}
		time.Sleep(200 * time.Millisecond) // запрос длиннее момента остановки
		_, _ = io.WriteString(w, "ok")
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	ctx, sigterm := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- ServeUntil(ctx, srv, ln, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range inFlight {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get("http://" + addr + "/")
			if err != nil {
				failed.Add(1)
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil || resp.StatusCode != http.StatusOK || string(body) != "ok" {
				failed.Add(1)
			}
		}()
	}
	select {
	case <-allStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("запросы не дошли до сервера")
	}
	sigterm() // SIGTERM: все 40 запросов в работе

	wg.Wait()
	if n := failed.Load(); n != 0 {
		t.Fatalf("оборвано запросов: %d из %d", n, inFlight)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("ServeUntil: %v", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("сервер не остановился")
	}
	if c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("после остановки порт должен быть закрыт")
	}
}

// Запрос длиннее таймаута остановки: сервер не ждёт его бесконечно.
func TestServeUntil_TimeoutBoundsShutdown(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	ctx, sigterm := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- ServeUntil(ctx, srv, ln, 100*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	go func() { _, _ = http.Get("http://" + ln.Addr().String() + "/") }()
	<-entered
	begin := time.Now()
	sigterm()
	select {
	case <-served:
	case <-time.After(3 * time.Second):
		t.Fatal("остановка не ограничена таймаутом")
	}
	close(release)
	if d := time.Since(begin); d > time.Second {
		t.Fatalf("остановка заняла %s при таймауте 100 мс", d)
	}
}
