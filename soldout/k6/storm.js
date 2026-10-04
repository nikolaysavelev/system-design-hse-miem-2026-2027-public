// storm: «12:00, старт продаж» — 0 → 1000 VU за 10 с, держим STORM_DURATION (2m; storm-short = 60s); 80 % hold, 20 % seatmap.
// Пороги заданы так, чтобы на lesson-1 прогон был КРАСНЫМ по p99 и ошибкам (пул 200 > max_connections 100,
// seatmap без кэша, seq scan по holds) — это исходная точка занятия 2.
import { check } from 'k6';
import { uuid, randomSeat, join, hold, seatmap } from './lib.js';

const DURATION = __ENV.STORM_DURATION || '2m';

export const options = {
  scenarios: {
    storm: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: 1000 },
        { duration: DURATION, target: 1000 },
        { duration: '10s', target: 0 },
      ],
      gracefulRampDown: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{name:hold}': ['p(99)<500'],
    'http_req_duration{name:seatmap}': ['p(99)<200'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
  discardResponseBodies: false,
};

export default function () {
  if (Math.random() < 0.2) {
    const r = seatmap(true);
    check(r, { 'seatmap 200': (res) => res.status === 200 });
    return;
  }
  const userId = uuid();
  const token = join(userId);
  if (!token) return;
  const r = hold(userId, token, randomSeat());
  check(r, { 'hold 201/409': (res) => res.status === 201 || res.status === 409 });
}
