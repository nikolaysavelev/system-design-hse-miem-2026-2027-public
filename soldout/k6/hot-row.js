// hot-row: «горячий ряд». Два подсценария (HOT_MODE=specific|any|both):
//   specific — 500 VU × 60 с бьют в 20 мест первого ряда (seat_id 1..20): ретрай ≤ 2 с джиттером 50–150 мс на 5xx;
//              ожидание после шага C: 0 таймаутов, p99 < 200 мс, 409 мгновенные (допустимы).
//   any      — 500 VU по одной итерации: POST /v1/holds/any в секторе HOT_SECTOR_NO (по умолчанию 2);
//              старт каждого VU со случайной задержкой 0–1 с; setup() доводит свободные места до HOT_ANY_FREE (20); ожидание: ровно 20 × 201,
//              остальное — 409 sector_exhausted, 0 ошибок 5xx.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter } from 'k6/metrics';
import { BASE_URL, EVENT_ID, uuid, join, hold, sectorID } from './lib.js';

const MODE = __ENV.HOT_MODE || 'both';
const HOT_SEATS = 20;
const HOT_SECTOR = sectorID(parseInt(__ENV.HOT_SECTOR_NO || '2', 10));
const ANY_FREE = parseInt(__ENV.HOT_ANY_FREE || '20', 10);
const anyCreated = new Counter('hold_any_created');
const anyExhausted = new Counter('hold_any_exhausted');
const json = { 'Content-Type': 'application/json' };

const scenarios = {};
if (MODE === 'specific' || MODE === 'both') {
  scenarios.specific = { executor: 'constant-vus', vus: 500, duration: '60s', exec: 'specific' };
}
if (MODE === 'any' || MODE === 'both') {
  scenarios.any = { executor: 'per-vu-iterations', vus: 500, iterations: 1, maxDuration: '60s', exec: 'any',
    startTime: MODE === 'both' ? '62s' : '0s' };
}

export const options = {
  scenarios,
  thresholds: {
    'http_req_duration{name:hold}': ['p(99)<200'],
    'http_req_failed{name:hold}': ['rate<0.01'],
    'http_req_failed{name:hold_any}': ['rate<0.01'],
    'http_req_duration{name:hold_any}': ['p(99)<500'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

function holdAny(userId, token, key) {
  return http.post(`${BASE_URL}/v1/holds/any`, JSON.stringify({ event_id: EVENT_ID, sector_id: HOT_SECTOR, user_id: userId }), {
    headers: Object.assign({ 'X-Admission-Token': token, 'Idempotency-Key': key }, json), tags: { name: 'hold_any' },
  });
}

function sectorFree() {
  const r = http.get(`${BASE_URL}/v1/events/${EVENT_ID}/seatmap`, { tags: { name: 'seatmap_summary' } });
  const sec = r.json('sectors').find((s) => s.id === HOT_SECTOR);
  return sec ? sec.free : -1;
}

// Доводим свободные места сектора до ANY_FREE через /holds/any (по 4 на пользователя — лимит FR-7).
export function setup() {
  if (!scenarios.any) return {};
  let free = sectorFree();
  let guard = 0;
  while (free > ANY_FREE && guard++ < 400) {
    const u = uuid();
    const t = join(u);
    for (let i = 0; i < 4 && free > ANY_FREE; i++) {
      const r = holdAny(u, t, uuid());
      if (r.status === 201) free--; else break;
    }
    if (guard % 25 === 0) free = sectorFree();
  }
  free = sectorFree();
  console.log(`setup: свободных мест в секторе ${HOT_SECTOR}: ${free}`);
  return { free };
}

// Пользователь входит в очередь один раз, а не на каждую попытку: токен кэшируется на VU.
let vuUser = null;
let vuToken = null;

export function specific() {
  if (!vuToken) {
    vuUser = uuid();
    vuToken = join(vuUser);
    if (!vuToken) return;
  }
  const userId = vuUser;
  const token = vuToken;
  const seatId = 1 + Math.floor(Math.random() * HOT_SEATS);
  let r;
  for (let attempt = 0; attempt < 3; attempt++) {
    r = hold(userId, token, seatId);
    if (r.status < 500) break;
    sleep(0.05 + Math.random() * 0.1); // джиттер 50–150 мс
  }
  check(r, {
    'hold 201/409/422 (победитель / занято / лимит 4 у победителя)': (res) => res.status === 201 || res.status === 409 || res.status === 422,
    'hold без 5xx': (res) => res.status < 500,
  });
  if (r.status === 422) { vuToken = null; } // лимит 4 — «новый» пользователь
}

export function any() {
  sleep(Math.random()); // 500 конкурентов в течение одной секунды, а не в одну миллисекунду (пул 40 соединений)
  const userId = uuid();
  const token = join(userId);
  if (!token) return;
  const key = uuid();
  const r = holdAny(userId, token, key);
  if (r.status === 201) anyCreated.add(1);
  if (r.status === 409) anyExhausted.add(1);
  check(r, {
    'any 201 или 409 sector_exhausted': (res) => res.status === 201 || (res.status === 409 && res.json('error.code') === 'sector_exhausted'),
    'any без 5xx': (res) => res.status < 500,
  });
  if (r.status === 201) {
    // повтор с тем же ключом → 200 и тот же hold (идемпотентность)
    const again = holdAny(userId, token, key);
    check(again, { 'any повтор → 200, тот же hold': (res) => res.status === 200 && res.json('id') === r.json('id') });
  }
}
