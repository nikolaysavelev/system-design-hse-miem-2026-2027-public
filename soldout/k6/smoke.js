// smoke: 10 VU × 60 с, полный путь покупателя: seatmap → queue/join → hold → order → pay.
// Ожидание на L1: зелёный. 409 на hold (место уже занято другим VU) — не ошибка, повторяем с другим местом.
import { check, sleep } from 'k6';
import { uuid, randomSeat, join, hold, createOrder, pay, seatmap } from './lib.js';

export const options = {
  vus: 10,
  duration: '60s',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:hold}': ['p(99)<500'],
    'http_req_duration{name:pay}': ['p(99)<1000'],
    'http_req_duration{name:seatmap}': ['p(95)<2000'],
    checks: ['rate>0.99'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const userId = uuid();

  const sm = seatmap();
  check(sm, { 'seatmap 200': (r) => r.status === 200 });

  const token = join(userId);
  if (!token) return;

  let h = null;
  for (let attempt = 0; attempt < 3 && !h; attempt++) {
    const res = hold(userId, token, randomSeat());
    if (res.status === 201) h = res.json();
    else check(res, { 'hold 409 (место занято) — допустимо': (r) => r.status === 409 });
  }
  if (!h) return;

  const o = createOrder(userId, [h.id]);
  check(o, { 'order 201': (r) => r.status === 201 });
  if (o.status !== 201) return;

  const p = pay(o.json('id'));
  check(p, { 'pay 200 → paid': (r) => r.status === 200 && r.json('status') === 'paid' });

  sleep(0.5);
}
