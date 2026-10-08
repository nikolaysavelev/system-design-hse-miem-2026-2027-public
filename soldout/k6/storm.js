// storm: «12:00, старт продаж» — 0 → 1000 VU за 10 с, держим STORM_DURATION (2m; storm-short = 60s); 80 % hold, 20 % карта сектора.
// Пороги заданы так, чтобы на lesson-1 прогон был КРАСНЫМ по p99 и ошибкам (пул 200 > max_connections 100,
// seatmap без кэша, seq scan по holds) — это исходная точка занятия 2.
import { check } from 'k6';
import { uuid, randomSeat, join, hold, seatmap } from './lib.js';

const DURATION = __ENV.STORM_DURATION || '2m';
const VUS = parseInt(__ENV.STORM_VUS || '1000', 10);
// Занятие 4: фоновый шторм под checkout идёт с фиксированной частотой STORM_RATE итераций/с (constant-arrival-rate):
// замкнутый шторм даже на 200 VU разгоняется до ~3 000 RPS и упирает PostgreSQL в его 1 CPU.
const RATE = parseInt(__ENV.STORM_RATE || '0', 10);

export const options = {
  scenarios: RATE > 0 ? {
    storm: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.min(VUS, 200),
      maxVUs: VUS,
    },
  } : {
    storm: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '10s', target: VUS },
        { duration: DURATION, target: VUS },
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
