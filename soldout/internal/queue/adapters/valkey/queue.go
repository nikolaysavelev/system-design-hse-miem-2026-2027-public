package valkey

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	vk "github.com/valkey-io/valkey-go"

	"github.com/nikolaysavelev/soldout/internal/queue/app"
)

// Queue — waiting room в Valkey: sorted set queue:{event} (score = время входа), inflight:{event},
// token bucket bucket:{event} (hash tokens/ts), множество событий queue:events, допуски admitted:{event}:{user}.
type Queue struct {
	client vk.Client
	admit  *vk.Lua
	reap   *vk.Lua
}

// admitScript: KEYS = queue, inflight, bucket; ARGV = now_ms, rate, k.
// Token bucket с ёмкостью = rate (одна секунда запаса), ZPOPMIN до min(k, tokens) пользователей → inflight.
const admitScript = `
local now = tonumber(ARGV[1]); local rate = tonumber(ARGV[2]); local k = tonumber(ARGV[3])
local b = redis.call('HMGET', KEYS[3], 'tokens', 'ts')
local tokens = tonumber(b[1]); local ts = tonumber(b[2])
if tokens == nil then tokens = rate end
if ts == nil then ts = now end
tokens = math.min(rate, tokens + (now - ts) * rate / 1000)
local n = math.min(k, math.floor(tokens))
local out = {}
if n > 0 then
  local popped = redis.call('ZPOPMIN', KEYS[1], n)
  for i = 1, #popped, 2 do
    redis.call('ZADD', KEYS[2], now, popped[i])
    out[#out + 1] = popped[i]
  end
end
redis.call('HSET', KEYS[3], 'tokens', tokens - #out, 'ts', now)
redis.call('PEXPIRE', KEYS[3], 3600000)
return out`

// reapScript: KEYS = queue, inflight; ARGV = deadline_ms. Залежавшиеся возвращаются в голову очереди (score 0).
const reapScript = `
local stale = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1])
for _, u in ipairs(stale) do
  redis.call('ZREM', KEYS[2], u)
  redis.call('ZADD', KEYS[1], 0, u)
end
return #stale`

// NewQueue создаёт адаптер.
func NewQueue(client vk.Client) *Queue {
	return &Queue{client: client, admit: vk.NewLuaScript(admitScript), reap: vk.NewLuaScript(reapScript)}
}

var _ app.Queue = (*Queue)(nil)

func qkey(e uuid.UUID) string    { return "queue:" + e.String() }
func ikey(e uuid.UUID) string    { return "queue:inflight:" + e.String() }
func bkey(e uuid.UUID) string    { return "queue:bucket:" + e.String() }
func akey(e, u uuid.UUID) string { return "admitted:" + e.String() + ":" + u.String() }

const eventsKey = "queue:events"

// Enqueue — ZADD NX; при равных score порядок лексикографический по user_id (честно: не строго FIFO в одну миллисекунду).
func (q *Queue) Enqueue(ctx context.Context, eventID, userID uuid.UUID, now time.Time) (int64, error) {
	cmds := vk.Commands{
		q.client.B().Zadd().Key(qkey(eventID)).Nx().ScoreMember().ScoreMember(float64(now.UnixMilli()), userID.String()).Build(),
		q.client.B().Sadd().Key(eventsKey).Member(eventID.String()).Build(),
	}
	for _, r := range q.client.DoMulti(ctx, cmds...) {
		if err := r.Error(); err != nil {
			return 0, fmt.Errorf("queue/valkey: enqueue: %w", err)
		}
	}
	return q.Position(ctx, eventID, userID)
}

// Position — ZRANK + 1; 0 если не в очереди.
func (q *Queue) Position(ctx context.Context, eventID, userID uuid.UUID) (int64, error) {
	res := q.client.Do(ctx, q.client.B().Zrank().Key(qkey(eventID)).Member(userID.String()).Build())
	if err := res.Error(); err != nil {
		if vk.IsValkeyNil(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("queue/valkey: zrank: %w", err)
	}
	n, err := res.AsInt64()
	if err != nil {
		return 0, nil
	}
	return n + 1, nil
}

// Admit — Lua: token bucket + ZPOPMIN → inflight.
func (q *Queue) Admit(ctx context.Context, eventID uuid.UUID, nowMs int64, rate float64, k int) ([]uuid.UUID, error) {
	res := q.admit.Exec(ctx, q.client, []string{qkey(eventID), ikey(eventID), bkey(eventID)},
		[]string{strconv.FormatInt(nowMs, 10), strconv.FormatFloat(rate, 'f', -1, 64), strconv.Itoa(k)})
	if err := res.Error(); err != nil {
		return nil, fmt.Errorf("queue/valkey: admit: %w", err)
	}
	members, err := res.AsStrSlice()
	if err != nil {
		return nil, fmt.Errorf("queue/valkey: admit result: %w", err)
	}
	out := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if u, err := uuid.Parse(m); err == nil {
			out = append(out, u)
		}
	}
	return out, nil
}

// Done — ZREM из inflight.
func (q *Queue) Done(ctx context.Context, eventID, userID uuid.UUID) error {
	if err := q.client.Do(ctx, q.client.B().Zrem().Key(ikey(eventID)).Member(userID.String()).Build()).Error(); err != nil {
		return fmt.Errorf("queue/valkey: done: %w", err)
	}
	return nil
}

// Reap — вернуть залежавшихся из inflight в голову очереди.
func (q *Queue) Reap(ctx context.Context, eventID uuid.UUID, nowMs, olderThanMs int64) (int64, error) {
	res := q.reap.Exec(ctx, q.client, []string{qkey(eventID), ikey(eventID)}, []string{strconv.FormatInt(nowMs-olderThanMs, 10)})
	if err := res.Error(); err != nil {
		return 0, fmt.Errorf("queue/valkey: reap: %w", err)
	}
	n, _ := res.AsInt64()
	return n, nil
}

// Sizes — ZCARD очереди и inflight.
func (q *Queue) Sizes(ctx context.Context, eventID uuid.UUID) (int64, int64, error) {
	rs := q.client.DoMulti(ctx, q.client.B().Zcard().Key(qkey(eventID)).Build(), q.client.B().Zcard().Key(ikey(eventID)).Build())
	a, err1 := rs[0].AsInt64()
	b, err2 := rs[1].AsInt64()
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("queue/valkey: sizes: %v %v", err1, err2)
	}
	return a, b, nil
}

// Events — SMEMBERS queue:events.
func (q *Queue) Events(ctx context.Context) ([]uuid.UUID, error) {
	members, err := q.client.Do(ctx, q.client.B().Smembers().Key(eventsKey).Build()).AsStrSlice()
	if err != nil {
		return nil, fmt.Errorf("queue/valkey: events: %w", err)
	}
	out := make([]uuid.UUID, 0, len(members))
	for _, m := range members {
		if u, err := uuid.Parse(m); err == nil {
			out = append(out, u)
		}
	}
	return out, nil
}

// SetAdmitted — токен допуска пользователя.
func (q *Queue) SetAdmitted(ctx context.Context, eventID, userID uuid.UUID, token string, ttl time.Duration) error {
	if err := q.client.Do(ctx, q.client.B().Set().Key(akey(eventID, userID)).Value(token).Px(ttl).Build()).Error(); err != nil {
		return fmt.Errorf("queue/valkey: set admitted: %w", err)
	}
	return nil
}

// GetAdmitted — токен допуска или "" если ещё не допущен.
func (q *Queue) GetAdmitted(ctx context.Context, eventID, userID uuid.UUID) (string, error) {
	res := q.client.Do(ctx, q.client.B().Get().Key(akey(eventID, userID)).Build())
	if err := res.Error(); err != nil {
		if vk.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("queue/valkey: get admitted: %w", err)
	}
	return res.ToString()
}
