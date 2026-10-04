// Общие помощники k6-сценариев soldout.
import http from 'k6/http';
import { check, sleep } from 'k6';

// 409 (место занято/продано) — ожидаемый исход под нагрузкой, не считается ошибкой http_req_failed.
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }, 409));

export const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
export const EVENT_ID = __ENV.EVENT_ID || '10000000-0000-4000-8000-000000000001';
export const SEATS_PER_EVENT = parseInt(__ENV.SEATS_PER_EVENT || '40000', 10);
// Места мероприятия 1 имеют id 1..SEATS_PER_EVENT (см. seed/seed.sql).
export const SEAT_MIN = 1;
export const SEAT_MAX = SEATS_PER_EVENT;
export const SECTORS_PER_EVENT = SEATS_PER_EVENT / 1000; // по 1000 мест в секторе (seed/seed.sql)
export const EVENT_NO = parseInt(__ENV.EVENT_NO || '1', 10);

// Детерминированные id секторов из seed: 30000000-0000-4000-8000-<hex(event*1000+sector)>
export function sectorID(sectorNo) {
  return '30000000-0000-4000-8000-' + (EVENT_NO * 1000 + sectorNo).toString(16).padStart(12, '0');
}
export function randomSector() {
  return sectorID(1 + Math.floor(Math.random() * SECTORS_PER_EVENT));
}

export function uuid() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16);
  });
}

export function randomSeat(min = SEAT_MIN, max = SEAT_MAX) {
  return min + Math.floor(Math.random() * (max - min + 1));
}

const json = { 'Content-Type': 'application/json' };

export const QUEUE_ENABLED = (__ENV.QUEUE_ENABLED || 'false') === 'true';

// Токен допуска. QUEUE_ENABLED=false — выдаётся сразу; true — join ставит в очередь, дальше опрос status
// раз в 500 мс до допуска (stepD, ADR-002). Возвращает null, если не допустили за 60 с.
export function join(userId) {
  const res = http.post(`${BASE_URL}/v1/events/${EVENT_ID}/queue/join`, JSON.stringify({ user_id: userId }), {
    headers: json, tags: { name: 'queue_join' },
  });
  check(res, { 'queue/join 200': (r) => r.status === 200 });
  if (res.status !== 200) return null;
  const token = res.json('token');
  if (token) return token;
  if (!QUEUE_ENABLED) return null;
  for (let i = 0; i < 120; i++) {
    sleep(0.5);
    const st = http.get(`${BASE_URL}/v1/events/${EVENT_ID}/queue/status?user_id=${userId}`, { tags: { name: 'queue_status' } });
    if (st.status === 200 && st.json('admitted')) return st.json('token');
  }
  return null;
}

// hold: 201 — успех, 409 — занято/продано (ожидаемо под нагрузкой), 422 — лимит.
export function hold(userId, token, seatId) {
  return http.post(`${BASE_URL}/v1/holds`, JSON.stringify({ event_id: EVENT_ID, seat_id: seatId, user_id: userId }), {
    headers: Object.assign({ 'X-Admission-Token': token }, json), tags: { name: 'hold' },
  });
}

export function createOrder(userId, holdIds) {
  return http.post(`${BASE_URL}/v1/orders`, JSON.stringify({ hold_ids: holdIds, user_id: userId }), {
    headers: Object.assign({ 'Idempotency-Key': uuid() }, json), tags: { name: 'order' },
  });
}

export function pay(orderId) {
  return http.post(`${BASE_URL}/v1/orders/${orderId}/pay`, null, { headers: json, tags: { name: 'pay' } });
}

// Сводка по секторам (с занятия 2 — маленький ответ) и карта одного сектора (≈ 32 КБ, ETag).
export function seatmapSummary() {
  return http.get(`${BASE_URL}/v1/events/${EVENT_ID}/seatmap`, { tags: { name: 'seatmap_summary' } });
}
export function seatmap(discard = false, sector = null) {
  const sec = sector || randomSector();
  return http.get(`${BASE_URL}/v1/events/${EVENT_ID}/sectors/${sec}/seatmap`, { tags: { name: 'seatmap' }, responseType: discard ? 'none' : 'text' });
}
