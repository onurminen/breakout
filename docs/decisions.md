# Decisions

Why the system is built as it is. When a decision changes, update its row and the document describing it.

## System

| Decision | Alternatives | Why |
|---|---|---|
| Go for all backend services | Rust, TypeScript, Python, Java/Scala | Fast, simple concurrency with goroutines, single static binaries; new to the team, which was a goal |
| Kafka between services | Go channels in one process, NATS, Redis Streams, Redpanda | Durable log that can be replayed, consumer groups for scaling, several independent readers per topic. |
| Engine commits no offsets; a newly assigned partition is re-read from the start | Commit offsets and save engine state | No state store needed; partitions only move when instances join or leave, and the experiments start all engines before ingestion |
| Partition by symbol | Partition by time | The EMA is a step-by-step calculation per symbol, while different symbols are independent |
| 12 partitions | Any other fixed number | More than the largest engine count planned to be tested (8); divides evenly among 1, 2, 3, 4 and 6 engines |
| 1 partition for `ema-results` and `advisories` | 3 or 12 partitions | Partitions only let several sinks share the work, and ~1.9M results per week ([windows check](data.md#empty-windows-windows)) need one sink; each API instance reads every partition anyway. One partition also gives one global order |
| Protobuf messages | JSON, FlatBuffers, hand-made binary | Compact and fast with little effort; generates Go and TypeScript types from one schema |
| Sink writes to Postgres | Engine writes to Postgres itself | The engine never waits for the database; Kafka buffers results if Postgres is slow; no history gaps after a crash |
| Prices as `double` | Decimal types | The EMA weights 2/39 and 2/101 are not exact decimals anyway |
| Idempotent writes keyed by (symbol, window start) | Exactly-once transactions | Simple, and enough with at-least-once delivery |
| Postgres | TimescaleDB, QuestDB, ClickHouse, Redis | ~1.9M results per week ([windows check](data.md#empty-windows-windows)) is small for an indexed table; TimescaleDB remains a drop-in upgrade |
| Live results Kafka → API → WebSocket | API polls Postgres | No database write, read or polling delay on the way to the user |
| API forwards repeated live results after an engine replay | API keeps a deduplication cache | The API stays stateless; clients deduplicate by symbol, window start, and message type, while Postgres absorbs duplicate rows |
| New clients subscribe, wait for confirmation, then fetch history | Fetch history before subscribing | Buffer live messages during the history fetch and merge by symbol, window start, and message type; refresh history if the sink is still catching up |
| Latency ends when a result is published to Kafka | Until it reaches the browser | The browser's clock is not synchronized with the servers' |
| Prometheus, Grafana, cAdvisor | A custom logging or evaluation service | Performance questions need metrics, not logs; standard tools |

## Data and ingestion

| Decision | Alternatives | Why |
|---|---|---|
| Day from `Date` | `Trading date`, as the assignment description names | `Trading date` is empty for about 92% of price events ([check](data.md#trading-date-tradingdate)) |
| Price from `Last` | The `Close` column | The assignment's "Close" means the last price in the window |
| Drop prices ≤ 0 | Keep them | A zero pulls both EMAs towards 0 |
| Filter and shrink events in ingestion | Filter in the engine or in Kafka | 79% of lines are dropped before any serialization or network cost |
| Send events in file order | Sort by event time first | Each symbol is already in order ([order check](data.md#order-within-each-symbol-order)); sorting 5 GB is batch processing; sorting would put Wednesday's 01:00 events into a 01:00 window and change later EMAs |
| Keep repeated prices | Drop them | A repeat still means "the price is X now" and keeps a window from being empty |
| Hand-written parser with zero allocations per line | Go's `encoding/csv` | `encoding/csv` allocates for every line |

## Windowing

| Decision | Alternatives | Why |
|---|---|---|
| Event-time windows closed by watermarks | Windows by arrival order; close a symbol's window on its own next event | Arrival order gives wrong results under disorder; per-symbol closing delays quiet symbols for hours |
| Grace period 1 s | 0 s, 10 s, 90 s | Smallest value with zero late events on every day except Wednesday's anomaly, which no grace period catches ([grace check](data.md#grace-period-grace)). 0 s gives 24–119 late events per day; more than 1 s only delays results |
| Drop and count late events | Use them anyway | Wednesday's 5,506 01:00 events would otherwise put stale prices into the wrong windows |
| Skip empty windows | Carry the last price forward | Carrying forward creates crossovers without trading; 73% of windows are empty ([windows check](data.md#empty-windows-windows)) |
| Carry EMA state across days | Reset every midnight | A daily reset would keep EMA100 warming up all day, so almost every advisory would be the artificial first-window buy |
| First window exactly as in the assignment, flagged `warmup` | Start the EMA at the first price; suppress the advisory | Matches the assignment for grading; the artifact stays visible and documented |

## Evaluation

| Decision | Alternatives | Why |
|---|---|---|
| E1 and E2 run on a pre-filled `ticks` topic | Run engines live alongside ingestion | Ingestion produces ~220k records/s, one engine reads ~3M/s: live, every engine count would show ingestion's rate. Live throughput is still measured, in E3 |
| E2 varies the CPU limit of one engine (0.5, 1, 2, 4) | Every engine limited to 1 CPU with 1, 2, 4, 8 engines; or E1 itself run with 1 CPU per engine | E1 already varies the number of instances, so E2 measures the other dimension, cores per instance. The alternatives would show scale-out with a fixed budget per instance, as on separate machines, and remain candidates for further runs |
| End-to-end check over the whole week for twelve symbols | One day | On the first two days the only advisories are warm-up buys; real crossovers, late events and midnight all appear only over the week |
| 80 latency buckets, each ~18% wider than the previous | 20 buckets, each twice the previous; Prometheus native histograms | Percentiles are interpolated inside a bucket. With 20 buckets almost every event fell into 6.7–13.5 ms, so p95 and p99 came out just below that bucket's edge at every speed. 80 buckets cost only more series; native histograms would need extra Prometheus configuration |
| Kafka's initial rebalance delay 3 s | 0 s | Engines started together get their partitions in one assignment instead of re-reading partitions moved by later joins |

## Open questions

- **Parallel parsing.** Only needed if one core of parsing turns out to limit the whole pipeline. If so: parse numbered chunks of the file in parallel and send them on in their original order.
