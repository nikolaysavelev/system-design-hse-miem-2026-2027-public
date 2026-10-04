// waiting-room: 2000 VU опрашивают очередь. На L1 join всегда выдаёт токен (позиция 0) — сценарий
// служит базовой линией; настоящая очередь с admission rate появляется на занятии 2.
import { check, sleep } from 'k6';
import { uuid, join } from './lib.js';

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
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:queue_join}': ['p(99)<300'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const token = join(uuid());
  check(token, { 'токен выдан': (t) => !!t });
  sleep(5); // «опрос раз в 5 с»
}
