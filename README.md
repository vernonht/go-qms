# Queue Management System (QMS) – Implementation Summary

## Architecture

External dependencies: `github.com/gorilla/websocket`

## Architecture

| Package | Responsibility |
|---|---|
| `internal/order` | `Order` struct, `Type` (Normal/VIP), `Status` enum |
| `internal/queue` | Thread-safe priority queue — VIP → FIFO, Normal → FIFO, `EnqueueFront` for bot interruptions |
| `internal/controller` | Orchestrates bots, queue, and completed list; exposes `NewOrder`, `AddBot`, `RemoveBot`, `State`, `Subscribe`/`Unsubscribe` |

## Key Design Decisions

- Each bot is a goroutine that loops: dequeue → process (`time.After`) → complete, or stop-signal → return order
- `stopCh` is a closed channel so any pending `select` fires immediately on bot removal
- `EnqueueFront` puts a returned order at the head of its priority group (VIP → pos 0, Normal → after last VIP) so priority is maintained after a bot interruption
- Order and bot IDs use `sync/atomic` for lock-free increment; everything else is mutex-guarded
- `Subscribe`/`Unsubscribe` expose a fan-out notification channel so observers (e.g. WebSocket handlers) are signalled on every state change without polling

## Tests — 27 total, all with `-race`

| Package | Tests |
|---|---|
| `internal/order` | 4 unit tests |
| `internal/queue` | 10 unit tests (including concurrent enqueue/dequeue) |
| `internal/controller` | 13 unit tests + 7 load tests |

Load tests cover: concurrent order submission (500 goroutines), all-orders-complete guarantee (100 orders, 5 bots), bot churn under load, VIP priority under high concurrency, concurrent bot add/remove, and end-to-end priority ordering.

## Scripts

| Script | Purpose |
|---|---|
| `script/build.sh` | Compiles to `./qms` |
| `script/test.sh` | Runs all tests with `-race` |
| `script/run.sh` | Runs demo → writes `result.txt` |

Set `PROCESS_SECONDS=10` (or unset) for the real 10-second-per-order behaviour. The demo defaults to `PROCESS_SECONDS=2` so CI completes quickly.

## CLI Modes

- `./qms --demo` — scripted simulation demonstrating all requirements, output written to `result.txt`
- `./qms` — interactive mode
- `./qms --server [addr]` — HTTP API server (default addr: `:8080`)

### HTTP API

Start the server:
```
./qms --server :8080
```

Swagger docs:
```
http://localhost:8080/swagger/index.html
```

If API annotations change, regenerate docs with:
```
$(go env GOPATH)/bin/swag init -g main.go
```

| Method | Path | Body | Response |
|---|---|---|---|
| `POST` | `/orders` | `{"type":"normal"\|"vip"}` | Created order object |
| `POST` | `/bots` | — | `{"bot_count": N}` |
| `DELETE` | `/bots` | — | `{"bot_count": N}` |
| `GET` | `/state` | — | JSON snapshot (HTTP) **or** live push stream (WebSocket) |

#### WebSocket stream

Upgrade `GET /state` to a WebSocket connection to receive a pushed JSON message on every state change (order created, bot assigned, order completed, bot added/removed). The server sends an initial snapshot immediately on connect, then a new message each time the system mutates.

```
# plain HTTP snapshot
curl http://localhost:8080/state

# WebSocket stream (requires websocat or similar)
websocat ws://localhost:8080/state
```

The pushed message format is identical to the HTTP JSON snapshot:
```json
{
  "pending":   [...],
  "bots":      [...],
  "completed": [...]
}
```

Example HTTP usage:
```
curl -X POST http://localhost:8080/bots
curl -X POST http://localhost:8080/orders -d '{"type":"vip"}'
curl http://localhost:8080/state
curl -X DELETE http://localhost:8080/bots
```

### Interactive Commands

| Command | Aliases | Action |
|---|---|---|
| `new normal` | `n`, `normal` | Add a Normal order |
| `new vip` | `v`, `vip` | Add a VIP order (jumps ahead of all Normal orders) |
| `+bot` | `+` `add bot` | Create a new cooking bot |
| `-bot` | `-` `remove bot` | Destroy the newest bot (order returns to PENDING) |
| `status` | `s` | Show current PENDING / BOTS / COMPLETE state |
| `quit` | `q`, `exit` | Exit |

## Requirements Coverage

| # | Requirement | How it is met |
|---|---|---|
| 1 | New Normal order → PENDING | `controller.NewOrder(order.Normal)` enqueues with status Pending |
| 2 | New VIP order → before all Normal, behind existing VIP | `queue.Enqueue` inserts VIP at `lastVIPIdx + 1` |
| 3 | Unique increasing order numbers | Atomic counter `orderSeq` |
| 4 | +Bot → processes PENDING, moves to COMPLETE after 10 s | Bot goroutine dequeues and uses `time.After(processDuration)` |
| 5 | Bot goes IDLE when no orders | Bot blocks on `workCh` channel when queue is empty |
| 6 | -Bot → newest destroyed, processing order returns to PENDING | `close(stopCh)` triggers `returnOrder` → `queue.EnqueueFront` |
| 7 | No data persistence | All state held in memory; no DB or file I/O |
