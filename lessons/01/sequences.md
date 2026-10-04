# `soldout`: sequence-диаграммы ключевых процессов (состояние `lesson-1`)

Пять процессов, которые надо понимать, чтобы читать код `internal/booking`, `internal/queue`, `internal/payment`, `internal/ticketing`. Участники соответствуют модулям; стрелки между модулями идут **только через пакеты `api`** (правило зависимостей из constitution). Пометки «L1» — как сейчас; «L2/L3/L7» — что изменится на следующих занятиях.

## 1. Waiting room: очередь и admission-токен

Зачем: ограничить поток в консистентную часть системы (admission rate), сделать обход очереди невозможным (токен проверяется на сервере, привязан к `user_id` и `event_id`, живёт ограниченное время).

```mermaid
sequenceDiagram
    autonumber
    actor U as Фанат (браузер)
    participant Q as queue (HTTP + app)
    participant V as Valkey
    participant PG as PostgreSQL
    participant B as booking

    U->>Q: POST /v1/events/{id}/queue/join {user_id}
    Note over Q,V: L1: очереди нет — токен выдаётся сразу (ADR-001, ограничение №7)
    Q->>Q: token = random(32)
    Q->>V: SET admission:{token} {event_id,user_id,expires_at} EX 600
    Q->>PG: INSERT admissions(token, event_id, user_id, expires_at)
    Q-->>U: 200 {position: 0, token, expires_at}

    Note over U,PG: L2: ZADD queue:{event} ts user_id → position,<br/>воркер выпускает 200 user/с (admission rate), клиент ждёт по SSE

    U->>B: POST /v1/holds  X-Admission-Token: token {event_id, seat_id, user_id}
    B->>Q: api.Validate(token, event_id, user_id)
    Q->>V: GET admission:{token}
    alt токен есть, не истёк, совпадают event_id и user_id
        Q-->>B: ok
        B-->>U: … (см. диаграмму 2)
    else нет / истёк / чужой
        Q-->>B: ErrAdmissionInvalid
        B-->>U: 403 admission_required
    end
```

## 2. Hold: удержание места и инвариант I1 (два конкурента на одно место)

Зачем: показать, где именно живёт «ноль двойных продаж» — в транзакции с advisory-lock и partial unique index, а не в проверке `if`.

```mermaid
sequenceDiagram
    autonumber
    actor A as Пользователь A
    actor Bo as Пользователь B
    participant H as booking (HTTP)
    participant S as booking.app.CreateHold
    participant C as catalog.api
    participant Q as queue.api
    participant PG as PostgreSQL

    par два запроса на seat 17 одновременно
        A->>H: POST /v1/holds {event, seat 17}
        Bo->>H: POST /v1/holds {event, seat 17}
    end
    H->>S: CreateHold(in)
    S->>C: GetEvent(event) → sales_state = open?
    S->>C: GetSeat(17) → seat.venue == event.venue?
    S->>Q: Validate(token)
    S->>PG: BEGIN
    S->>PG: SELECT pg_advisory_xact_lock(hashtext(event), 17)
    Note over S,PG: запрос B ждёт здесь, пока A не сделает COMMIT/ROLLBACK
    S->>PG: SELECT count(*) FROM holds WHERE user=A AND status IN (active,confirmed)
    S->>S: domain.CanHoldMore(n) — FR-7, лимит 4
    S->>PG: SELECT EXISTS ticket ON (event, 17)? — место уже продано?
    S->>PG: INSERT holds(id, event, 17, A, 'active', expires_at = now()+10m)
    Note over S,PG: uniq_active_hold (event, seat) WHERE status='active' — инвариант I1 в БД
    S->>PG: COMMIT
    S-->>H: Hold{active, expires_at}
    H-->>A: 201 {hold}

    Note over S,PG: теперь снимается advisory-lock — обрабатывается запрос B
    S->>PG: BEGIN … lock … INSERT holds(… 17, B …)
    PG-->>S: unique_violation uniq_active_hold
    S->>PG: ROLLBACK
    S-->>H: ErrSeatHeld
    H-->>Bo: 409 seat_held

    Note over A,PG: L2: на «горячем» ряду очередь на lock — тема занятия 2 (SKIP LOCKED / try-lock / кэш)
```

## 3. Заказ и оплата: order → pay → ticket → notification (L1 — всё синхронно)

Зачем: увидеть идемпотентность (`Idempotency-Key`), порты `PaymentGateway`/`TicketIssuer`, и то, почему `pay` на L1 медленный и хрупкий — это мотивация занятия 3 (outbox, события).

```mermaid
sequenceDiagram
    autonumber
    actor U as Фанат
    participant BH as booking (HTTP)
    participant B as booking.app
    participant PG as PostgreSQL
    participant P as payment (порт PaymentGateway)
    participant PSP as PSP-эмулятор
    participant T as ticketing (порт TicketIssuer)
    participant N as notification

    U->>BH: POST /v1/orders  Idempotency-Key: k1 {hold_ids:[h1,h2], user}
    BH->>B: CreateOrder
    B->>PG: SELECT order WHERE idempotency_key = k1
    alt уже есть с теми же hold_ids
        B-->>U: 200 {order} (повтор — тот же результат)
    else нет
        B->>PG: BEGIN, SELECT holds … FOR UPDATE
        B->>B: все holds active и не истекли? один пользователь?
        B->>PG: INSERT orders(pending, amount, k1), INSERT order_items
        B->>PG: COMMIT
        B-->>U: 201 {order pending}
    end

    U->>BH: POST /v1/orders/{id}/pay
    BH->>B: PayOrder
    B->>PG: SELECT order FOR UPDATE, статус pending? holds ещё usable?
    B->>P: Charge(order_id, amount, idempotency_key = order_id)
    P->>PG: SELECT payments WHERE idempotency_key — уже списано? → вернуть
    P->>PSP: charge (PSP_FAIL_RATE, PSP_DELAY_MS)
    alt PSP: успех
        PSP-->>P: ok, psp_ref
        P->>PG: INSERT payments(succeeded)
        P-->>B: PaymentResult{ok}
        B->>PG: UPDATE holds SET status='confirmed', UPDATE orders SET status='paid'
        B->>T: Issue(order_id, seats)  — синхронно, внутри pay
        T->>PG: INSERT tickets(order, seat, code) ON CONFLICT (order, seat) DO NOTHING
        T->>N: Send(user, 'ticket_issued', payload)
        N->>PG: INSERT notifications(...)
        N-->>T: ok
        T-->>B: tickets
        B-->>U: 200 {order paid, tickets}
    else PSP: отказ
        PSP-->>P: declined
        P->>PG: INSERT payments(failed)
        P-->>B: PaymentResult{declined}
        B->>PG: UPDATE orders SET status='failed', UPDATE holds SET status='released'
        B-->>U: 402 payment_declined (место снова свободно)
    end

    Note over B,N: L3: шаги 20–25 уезжают в outbox → Debezium → Kafka → ticketing/notification как сервисы.<br/>L7: весь процесс hold → pay → ticket становится сагой на Temporal с таймером 10 мин.
```

## 4. Истечение hold: expirer (фоновая горутина)

Зачем: место, удержанное и не оплаченное, должно вернуться в продажу без участия клиента.

```mermaid
sequenceDiagram
    autonumber
    participant E as booking.app.Expirer (каждые 5 с)
    participant PG as PostgreSQL
    participant M as metrics
    actor U as Фанат с истёкшим hold

    loop каждые EXPIRER_INTERVAL (5 с), до ctx.Done()
        E->>PG: UPDATE holds SET status='released' WHERE status='active' AND expires_at < now()
        Note over E,PG: L1: без индекса (event_id, status) — seq scan по holds (ADR-001, ограничение)
        PG-->>E: n строк
        E->>PG: UPDATE orders SET status='expired' WHERE pending AND все holds released
        E->>PG: SELECT count(*) active holds
        E->>M: holds_active gauge
    end

    U->>E: (позже) POST /v1/orders/{id}/pay
    E-->>U: 409 hold_expired — место уже могло уйти другому

    Note over E,PG: L7: истечение становится таймером внутри саги Temporal, expirer исчезает
```

## 5. Карта зала: как клиент видит статусы мест

Зачем: понять, что карта — проекция `catalog` + `booking` (через `booking.api.SeatStates`), и почему на L1 это самый тяжёлый запрос.

```mermaid
sequenceDiagram
    autonumber
    actor U as Фанат
    participant CH as catalog (HTTP)
    participant C as catalog.app
    participant B as booking.api.SeatStates
    participant PG as PostgreSQL

    U->>CH: GET /v1/events/{id}/seatmap
    CH->>C: SeatMap(event)
    C->>PG: SELECT sectors, seats WHERE venue = … (40 000 строк)
    C->>B: SeatStates(event)
    B->>PG: SELECT seat_id, status FROM holds WHERE event=… AND status IN (active, confirmed)
    Note over B,PG: L1: нет индекса (event_id, status), JOIN между модулями запрещён — два запроса, склейка в памяти
    B-->>C: map[seat_id]state
    C->>C: склеить 40 000 мест × состояние → JSON ≈ 800 КБ
    C-->>U: 200 seatmap (целиком)

    Note over U,PG: L2: карта по секторам, кэш в Valkey с инвалидацией по hold/release/confirm,<br/>ETag, и GET /seatmap/stream (SSE) с дельтами вместо повторных GET
```
