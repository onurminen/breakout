# Windowing

How the engine turns prices into EMA results and advisories. Reasons for each choice are in [decisions.md](decisions.md#windowing).

## What is computed

| The assignment says | Implementation |
|---|---|
| Group events by symbol, e.g. `RDSA.NL` | The symbol string including its exchange suffix is the key everywhere |
| Windows are 5 minutes long, do not overlap, and the first starts at 00:00 | An event belongs to window `event_time_ms / 300000` (integer division) |
| The price of a window is the last price event of the symbol in it | `Last` of the event with the highest event time in the window |
| `EMA_j = price · 2/(1+j) + previous EMA · (1 − 2/(1+j))`, previous EMA = 0 at the start | Implemented exactly like that, for j = 38 and j = 100 |
| Bullish crossover: EMA38 > EMA100 now, and EMA38 ≤ EMA100 in the previous window | Buy advisory |
| Bearish crossover: EMA38 < EMA100 now, and EMA38 ≥ EMA100 in the previous window | Sell advisory |
| A window is evaluated once the next window starts | The window closes when the watermark passes its end plus the grace period |
| Compute all symbols, but send updates only for subscribed ones | The engine computes everything; the API filters per WebSocket subscription |

## Event time and late events

An event belongs to the window its event time (`Date` + `Trading time`) falls into, not the one during which it arrives. An event whose window has already closed is **late**: the engine counts it (`late_events_total`) and drops it. With 1 s grace, the only late events in the week are Wednesday's 5,506 01:00 events ([data.md](data.md#anomalies)).

## Watermarks

- Ingestion remembers the highest event time it has sent. Each time it advances by one second, ingestion sends it as a watermark to **every** partition of `ticks`: "everything up to T has been sent".
- Watermarks travel behind the events they cover, in the same partitions.
- One goroutine owns each partition and its symbols, so it uses that partition's watermarks directly.
- At the end of the input, ingestion sends a final watermark with the largest possible value, closing all remaining windows.

A window from `t` to `t + 5 min` closes when

```
watermark ≥ t + 5 min + grace period      (grace period = GRACE_MS = 1000)
```

```
event times of PX1.FR:      10:01:10   10:03:55   10:04:59.6   10:05:02
window 10:00–10:05:         ├──────── collects these three ────────┤
watermark reaches 10:05:01  → window 10:00 closes, price = 10:04:59.6's Last
```

The event at 10:05:02 belongs to the next window. Until the watermark reaches 10:05:01, an event stamped 10:04:59.9 that arrives late is still included.

## State per symbol

The grace period is far shorter than a window, so a symbol has at most two open windows: the one waiting out its grace period and the current one. Per symbol the engine keeps:

- per open window: the latest price and its event time,
- EMA38 and EMA100 after the last closed window,
- whether the symbol has had a closed window yet.

Windows close in ascending order; each closing applies one EMA step.

## Edge cases

| Case | What happens |
|---|---|
| No price in a window | Skipped: no result, EMAs unchanged. The next crossover check compares with the last window that had a price |
| A symbol's first window | Computed as the assignment describes, from EMA = 0. Always produces a buy advisory (below), sent with `warmup=true` |
| Midnight | EMA state carries over to the next day |
| Zero or invalid price | Dropped by ingestion |
| EMAs almost equal | Strict comparisons as in the assignment, unit-tested for near-ties |

### Why the first window always says "buy"

Starting from 0, after the first price *P*:

```
EMA38  = P · 2/39  ≈ 0.051 · P
EMA100 = P · 2/101 ≈ 0.020 · P
```

EMA38 > EMA100 now, and both were 0 before (0 ≤ 0): a bullish crossover for every symbol.
