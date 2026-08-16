# arcticdispatch

Production-shaped Go backend for the **中欧北极快航 (China–Europe Arctic Express)**
route-dispatch centre. It coordinates cabin-slot allocation, booking, port-arrival
appointments, manifest declaration and arrival sign-off across five chained
flows:

```
订舱 (booking) → 配额冻结 (quota freeze) → 进港预约 (port appointment) → 舱单申报 (manifest) → 到港签收 (arrival)
```

The service is **self-contained**: only the Go standard library is used, state is
persisted to a JSON snapshot on disk, and background tasks reconcile expired
freezes and delayed voyages. No external accounts, hardware or live services are
required.

## Business rules implemented

- **Temperature control** — only temperature-confirmed 储能柜, 动力电池 and
  光伏组件 may occupy 冷藏 (refrigerated) slots; 普通货物 and unconfirmed cargo are
  rejected with `ErrTempControlFailed`.
- **Payment timeout** — a frozen slot auto-releases to the next waitlisted
  customer after 10 minutes (`payment_timeout`, configurable).
- **Dual-signature port change** — `改港` requires both the 调度专员 and the
  目的港代理 to sign; a booking may change port at most twice and never later
  than 72 hours before departure.
- **Berth-plan change** — `靠泊计划变更` re-sequences appointments so 旺季
  新能源订单 are prioritised and 普通货物 are postponed.
- **Last-slot contention** — when two sales managers race for the final batch of
  slots the write-lock serialises allocation: the first submitter is locked in and
  the other is pushed to the waitlist.
- **Ship delay recovery** — marking a voyage delayed auto re-appoints affected
  trucks, re-allocated by original priority; on restart the scheduler reconciles
  any freeze that expired or any appointment left mid-flight by a crash.

## Layout

```
cmd/server/            HTTP entrypoint (port 59041)
internal/domain/       cargo, voyage, booking, appointment, manifest, arrival
internal/store/        concurrency + JSON snapshot persistence & recovery
internal/clock/        injectable clock for deterministic tests
internal/config/       config.json + env loading
internal/cargo/        cargo registration & temperature confirmation
internal/booking/     订舱, payment, cancel, waitlist promotion, expiry reconcile
internal/dispatch/     voyages, delay, berth change, dual-sign port changes
internal/portapp/      进港预约, delay re-scheduling, priority re-sequencing
internal/manifest/     舱单申报 submit/declare/reject
internal/arrival/      到港签收
internal/scheduler/    background reconcile loop (expiry + delayed voyages)
internal/httpapi/      REST handlers
```

## Run locally

```bash
go run ./cmd/server            # listens on :59041, state in data/state.json
```

Override with environment variables: `CEAE_PORT`, `CEAE_STORE_PATH`,
`CEAE_PAYMENT_TIMEOUT`, `CEAE_SCHEDULER_INTERVAL`.

## Docker

```bash
docker build -t arcticdispatch .
docker run --rm -p 59041:59041 arcticdispatch
```

Multi-arch build (amd64 + arm64):

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t arcticdispatch .
```

## Main HTTP API (port 59041)

| Method | Path | Purpose |
| ------ | ---- | ------- |
| GET  | `/healthz` | liveness |
| POST | `/api/v1/cargo` | register cargo |
| POST | `/api/v1/cargo/{id}/confirm-temp` | warehouse supervisor confirms 温控 |
| POST | `/api/v1/voyages` | create voyage + slots |
| GET  | `/api/v1/voyages` / `/{id}` | list / inspect |
| POST | `/api/v1/voyages/{id}/delay` | mark delayed (auto re-appoint) |
| POST | `/api/v1/voyages/{id}/arrived` | mark arrived |
| POST | `/api/v1/voyages/{id}/berth-change` | re-sequence by priority |
| POST | `/api/v1/bookings` | 订舱 (idempotent) |
| POST | `/api/v1/bookings/{id}/pay` | pay within timeout |
| POST | `/api/v1/bookings/{id}/cancel` | cancel & release |
| POST | `/api/v1/bookings/{id}/port-change` | request 改港 |
| POST | `/api/v1/bookings/{id}/port-change/sign` | dual-sign |
| POST | `/api/v1/bookings/{id}/appointments` | 进港预约 |
| POST | `/api/v1/bookings/{id}/manifest` | submit 舱单 |
| POST | `/api/v1/manifests/{id}/declare` | declare 舱单 |
| POST | `/api/v1/bookings/{id}/arrival` | 到港签收 |

## Test

```bash
go test -timeout=120s -count=1 ./...
```

Tests cover normal paths, error paths, state transitions, concurrent
last-slot contention, cancellation and crash/restart recovery, with no
dependency on running external services.
