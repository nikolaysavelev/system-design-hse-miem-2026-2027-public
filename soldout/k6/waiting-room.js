// waiting-room (stepD, QUEUE_ENABLED=true): 2000 VU входят в очередь и опрашивают статус раз в секунду
// до допуска (ADMISSION_RATE в секунду), затем — один hold. Ожидание: p99 status < 5 мс, 0 ошибок,
// RPS к PostgreSQL ≈ ADMISSION_RATE × 3 (ровная полка на панели soldout-queue).
// При QUEUE_ENABLED=false — базовая линия занятия 1: join сразу выдаёт токен.
import http from 'k6/http';
import { check, sleep } from 'k6';
import { BASE_URL, EVENT_ID, uuid, hold, randomSeat, QUEUE_ENABLED } from './lib.js';

export const options = {
  scenarios: {
    waiting_room: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 2000 },
        { duration: '60s', target: 2000 },
        { duration: '5s', target: 0 },
      ],
      gracefulRampDown: '5s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:queue_join}': ['p(99)<300'],
    'http_req_duration{name:queue_status}': ['p(99)<50'],
    'http_req_duration{name:hold}': ['p(99)<500'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

const json = { 'Content-Type': 'application/json' };

export default function () {
  const userId = uuid();
  const res = http.post(`${BASE_URL}/v1/events/${EVENT_ID}/queue/join`, JSON.stringify({ user_id: userId }), { headers: json, tags: { name: 'queue_join' } });
  check(res, { 'join 200': (r) => r.status === 200 });
  if (res.status !== 200) return;
  let token = res.json('token');
  if (!token && QUEUE_ENABLED) {
    for (let i = 0; i < 90 && !token; i++) {
      sleep(1); // «опрос раз в секунду»
      const st = http.get(`${BASE_URL}/v1/events/${EVENT_ID}/queue/status?user_id=${userId}`, { tags: { name: 'queue_status' } });
      check(st, { 'status 200': (r) => r.status === 200 });
      if (st.status === 200 && st.json('admitted')) token = st.json('token');
    }
  }
  check(token, { 'допуск получен': (t) => !!t });
  if (!token) return;
  const h = hold(userId, token, randomSeat());
  check(h, { 'hold 201/409': (r) => r.status === 201 || r.status === 409 });
  sleep(5);
}
