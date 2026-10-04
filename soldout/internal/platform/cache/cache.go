// Package cache — клиент Valkey (valkey-go, RESP3).
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"
)

// New подключается к Valkey и проверяет соединение PING'ом.
func New(ctx context.Context, addr string) (valkey.Client, error) {
	c, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{addr}, DisableCache: true})
	if err != nil {
		return nil, fmt.Errorf("cache: connect %s: %w", addr, err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Do(pingCtx, c.B().Ping().Build()).Error(); err != nil {
		c.Close()
		return nil, fmt.Errorf("cache: ping %s: %w", addr, err)
	}
	return c, nil
}

// Ping — проверка живости для /readyz.
func Ping(ctx context.Context, c valkey.Client) error {
	return c.Do(ctx, c.B().Ping().Build()).Error()
}
