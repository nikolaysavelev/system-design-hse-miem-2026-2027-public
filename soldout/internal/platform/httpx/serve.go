package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// ServeUntil обслуживает запросы на ln до отмены ctx, затем останавливает сервер мягко: новые соединения
// не принимаются, начатые запросы дорабатывают до timeout. Так SIGTERM при выкатке не обрывает ни одного ответа.
// В Kubernetes перед SIGTERM стоит пауза preStop: за это время под уходит из endpoints и новых запросов уже нет.
func ServeUntil(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration, logger *slog.Logger) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	logger.Info("остановка: ждём завершения запросов", "timeout", timeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP-сервер остановлен принудительно", "err", err)
	}
	<-errc
	return nil
}
