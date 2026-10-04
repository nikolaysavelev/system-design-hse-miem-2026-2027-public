// Package valkey — кэш карты сектора: hash seatmap:{event}:{sector} с полями d (JSON), v (версия данных d)
// и w (желаемая версия после инвалидации). Запись — Lua CAS: только если версия не меньше сохранённой v;
// инвалидация поднимает w, и данные считаются stale, пока v < w (stale-while-revalidate).
// TTL — страховка от потерянного события, а не механизм консистентности.
package valkey

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	vk "github.com/valkey-io/valkey-go"
)

// Cache — реализация app.Cache.
type Cache struct {
	client vk.Client
	set    *vk.Lua
	inval  *vk.Lua
}

const setScript = `
local cur = redis.call('HGET', KEYS[1], 'v')
if cur and tonumber(cur) > tonumber(ARGV[1]) then return 0 end
redis.call('HSET', KEYS[1], 'v', ARGV[1], 'd', ARGV[2])
redis.call('PEXPIRE', KEYS[1], ARGV[3])
return 1`

const invalidateScript = `
local want = redis.call('HGET', KEYS[1], 'w')
if want and tonumber(want) >= tonumber(ARGV[1]) then return 0 end
redis.call('HSET', KEYS[1], 'w', ARGV[1])
if redis.call('TTL', KEYS[1]) < 0 then redis.call('PEXPIRE', KEYS[1], ARGV[2]) end
return 1`

// New создаёт кэш.
func New(client vk.Client) *Cache {
	return &Cache{client: client, set: vk.NewLuaScript(setScript), inval: vk.NewLuaScript(invalidateScript)}
}

func key(eventID, sectorID uuid.UUID) string {
	return "seatmap:" + eventID.String() + ":" + sectorID.String()
}

// Get — данные и версия; ok=false при промахе; stale=true, если желаемая версия w больше версии данных v.
func (c *Cache) Get(ctx context.Context, eventID, sectorID uuid.UUID) ([]byte, int64, bool, bool, error) {
	res := c.client.Do(ctx, c.client.B().Hmget().Key(key(eventID, sectorID)).Field("v", "d", "w").Build())
	if err := res.Error(); err != nil {
		return nil, 0, false, false, fmt.Errorf("catalog/valkey: hmget: %w", err)
	}
	arr, err := res.ToArray()
	if err != nil || len(arr) != 3 {
		return nil, 0, false, false, fmt.Errorf("catalog/valkey: hmget shape: %w", err)
	}
	vs, err1 := arr[0].ToString()
	ds, err2 := arr[1].ToString()
	if err1 != nil || err2 != nil {
		return nil, 0, false, false, nil // промах
	}
	v, err := strconv.ParseInt(vs, 10, 64)
	if err != nil {
		return nil, 0, false, false, nil
	}
	stale := false
	if ws, err := arr[2].ToString(); err == nil {
		if w, err := strconv.ParseInt(ws, 10, 64); err == nil && w > v {
			stale = true
		}
	}
	return []byte(ds), v, true, stale, nil
}

// Set — CAS по версии.
func (c *Cache) Set(ctx context.Context, eventID, sectorID uuid.UUID, payload []byte, version int64, ttl time.Duration) error {
	res := c.set.Exec(ctx, c.client, []string{key(eventID, sectorID)}, []string{strconv.FormatInt(version, 10), string(payload), strconv.FormatInt(ttl.Milliseconds(), 10)})
	if err := res.Error(); err != nil {
		return fmt.Errorf("catalog/valkey: set: %w", err)
	}
	return nil
}

// Invalidate — поднять желаемую версию: данные остаются и отдаются как stale до фонового обновления.
func (c *Cache) Invalidate(ctx context.Context, eventID, sectorID uuid.UUID, version int64) error {
	res := c.inval.Exec(ctx, c.client, []string{key(eventID, sectorID)}, []string{strconv.FormatInt(version, 10), strconv.FormatInt((5 * time.Minute).Milliseconds(), 10)})
	if err := res.Error(); err != nil {
		return fmt.Errorf("catalog/valkey: invalidate: %w", err)
	}
	return nil
}
