// hot-row: «горячий ряд» — 500 VU одновременно бьют в 20 мест первого ряда первого сектора.
// На L1 конкуренты выстраиваются в очередь на advisory-lock места; p99 растёт, большинство получает 409.
import { check } from 'k6';
import { uuid, join, hold } from './lib.js';

const HOT_SEATS = 20; // места 1..20 (сектор 1, ряд 1)

export const options = {
  scenarios: {
    hot_row: {
      executor: 'constant-vus',
      vus: 500,
      duration: '60s',
    },
  },
  thresholds: {
    'http_req_duration{name:hold}': ['p(99)<500'],
    http_req_failed: ['rate<0.01'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

export default function () {
  const userId = uuid();
  const token = join(userId);
  if (!token) return;
  const seatId = 1 + Math.floor(Math.random() * HOT_SEATS);
  const r = hold(userId, token, seatId);
  check(r, {
    'hold 201 (победитель)': (res) => res.status === 201,
    'hold 409 (место занято)': (res) => res.status === 409,
  });
}
