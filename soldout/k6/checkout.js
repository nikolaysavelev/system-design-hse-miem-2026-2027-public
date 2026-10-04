// checkout: полный путь покупки (занятие 4) — join → hold (случайное место) → order → pay (Idempotency-Key) → билеты.
// 150 VU × 90 с на мероприятии 2 (EVENT_NO=2), чтобы фоновый шторм на мероприятии 1 (make checkout-storm) не выбирал
// те же места, а конкурировал только за пул соединений и CPU. Порог pay p99 < 800 мс — бюджет NFR-1 без учёта PSP
// с запасом на PSP 300 мс; на ветке demo/l4-problem он красный: почтовый шлюз стоит в пути запроса.
import { check } from 'k6';
import { uuid, randomSeat, join, hold, createOrder, pay, tickets } from './lib.js';

export const options = {
  scenarios: {
    checkout: {
      executor: 'constant-vus',
      vus: parseInt(__ENV.CHECKOUT_VUS || '150', 10),
      duration: __ENV.CHECKOUT_DURATION || '90s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:pay}': ['p(99)<800'],
    'http_req_duration{name:order}': ['p(99)<500'],
    'http_req_duration{name:tickets}': ['p(99)<500'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const userId = uuid();
  const token = join(userId);
  if (!token) return;

  let h = null;
  for (let attempt = 0; attempt < 5 && !h; attempt++) {
    const res = hold(userId, token, randomSeat());
    if (res.status === 201) h = res.json();
    else check(res, { 'hold 409 (место занято) — допустимо': (r) => r.status === 409 });
  }
  if (!h) return;

  const o = createOrder(userId, [h.id]);
  check(o, { 'order 201': (r) => r.status === 201 });
  if (o.status !== 201) return;

  const p = pay(o.json('id'), uuid());
  check(p, { 'pay 200 → paid': (r) => r.status === 200 && r.json('status') === 'paid' });
  if (p.status !== 200) return;

  const t = tickets(userId);
  check(t, { 'tickets 200, есть билет': (r) => r.status === 200 && (r.json('tickets') || []).length > 0 });
}
