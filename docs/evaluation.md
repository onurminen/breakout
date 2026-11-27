# Evaluation

What is measured and how correctness is tested.

## Metrics

Every Go service exposes `/metrics` for Prometheus; cAdvisor adds CPU and memory per container, including Kafka's. Dropped lines are counted per parser reason (`no_price`, `bad_price`, `bad_time`, `bad_line`). The engine counts `engine_records_total` (everything read from `ticks`), `engine_events_total`, `late_events_total`, `engine_results_total` and `engine_advisories_total{side,warmup}`. The sink counts new rows per table (`sink_rows_total{table}`) and records batch sizes (`sink_batch_rows`) and insert times (`sink_insert_seconds`).

The Grafana dashboard `breakout` ([`deploy/grafana/`](../deploy/grafana/provisioning/dashboards/breakout.json)) shows a run at a glance: replay progress, totals, events/s overall and per engine replica, both latencies as p50/p95/p99, consumer lag, sink rows/s, and CPU and memory per container.

**Consumer lag.** The sink's lag comes from `kafka-exporter` (`kafka_consumergroup_lag`). The engine commits no offsets, so its group lag would always be the whole topic; instead, its lag is the end offset of `ticks` minus `engine_records_total`. The two are scraped at slightly different moments, so while data flows it can show a few seconds' worth of records.

## Latency

Both latencies compare two moments on the wall clock (all services share one machine), never event times.

- **Per-event latency** = time the engine processes an event − that event's `ingest_time_ns` (`engine_event_latency_seconds`).
- **Window-result latency** = a result's `published_ns` − `ingest_time_ns` of the watermark that closed its window (`engine_window_result_latency_seconds`). It excludes the window length and grace period, which are rules of the computation, not slowness. `published_ns` is when the result is handed to the Kafka producer.

Both histograms have 80 exponential buckets from 100 µs to 60 s, each about 18% wider than the previous. `histogram_quantile` interpolates linearly inside a bucket, so a reported percentile is accurate to about that width.

## Correctness

1. **Unit tests** for the computation: crossovers, touching without crossing, empty and late windows, disorder inside a window, first window, zero price, midnight, near-equal EMAs.
2. **Whole week through the engine core** without Kafka (`tools/enginecheck`): 1,855,645 results, exactly the number of windows with a price from the [windows check](data.md#empty-windows-windows), and 5,506 late events, exactly Wednesday's 01:00 events.
3. **End-to-end check** through the whole Compose pipeline (`go run ./tools/experiments e2e`): ingestion replays the whole week for twelve symbols (`SYMBOLS`), two equities and two indices from each exchange. Once the sink has stored every result, `tools/enginecheck -symbols … -db …` computes the same symbols without Kafka and compares both Postgres tables with its results row by row, comparing the doubles exactly. The whole week is needed because the first real crossovers come on Wednesday, after the warm-up; it also covers late events and EMA state across midnight.

## Experiments

Each experiment is one command, `go run ./tools/experiments e1|e2|e3`. It starts from an empty stack, runs every setting three times, and writes to `report/data/`: `<experiment>-summary.csv` with one row per run, and one time-series file per run (`seconds,series,value`).

For a quick manual check, `go run ./tools/experiments e3-fast` runs only E3's
maximum-speed setting once. It writes separate `e3-fast-*` files and is not a
replacement for the repeated E3 benchmark.

| # | Experiment | What varies | Input | Reported |
|---|---|---|---|---|
| E1 | Engine scale-out | 1, 2, 4, 8 engine instances, no CPU limit | Whole week, pre-filled | events/s, CPU seconds and cores, peak memory of the engines |
| E2 | CPU budget of one engine | `cpus:` 0.5, 1, 2, 4 for a single engine | Whole week, pre-filled | same as E1 |
| E3 | Replay speed | 1×, 10×, 60×, as fast as possible; one engine and the sink | Monday live, paced runs from 09:00 for 10 minutes | events/s, both latencies (p50, p95, p99), cores per container |

**Pre-filled topic (E1, E2).** Ingestion alone produces about 220k records/s, while one engine reads about 3 million records/s from a full topic, so in a live run the engines would only wait for ingestion. E1 and E2 therefore fill `ticks` with the whole week first and then start the engines on the full topic; each run reads it from the start, since the engine commits no offsets. Throughput is measured between the first scrape (every 250 ms) in which the engines have read anything and the first in which they have read the whole topic, so container startup and joining the group are left out. One engine reads Monday alone in about 5 s, too short to time precisely; the whole week takes several times longer. A run fails if the engines read more records than the topic holds, which would mean a partition was re-read after a rebalance; Kafka's initial rebalance delay (3 s) lets engines started together join before the partitions are assigned. Live end-to-end throughput, limited by ingestion, is E3's "as fast as possible" row.

**Paced runs (E3).** `REPLAY_FROM` makes ingestion replay the night before 09:00 as fast as possible and pace from there. Latencies and throughput are taken over the 10 minutes after pacing starts, from the histograms' increase over exactly that window.

Further settings that may be worth running are listed with the E2 decision in [decisions.md](decisions.md#evaluation).

## Results

Measured on one machine with 12 cores and 15 GB of memory; the CSV files in `report/data/` are the source of every number here. Values are medians of three runs, with the range in parentheses where it matters. Throughput varied by 10–20% between sessions on different days, so only runs from the same session are compared with each other: E2 ran on 20.11.2026, E1 and E3 on 21.11.2026.

### E1: engine scale-out (whole week, pre-filled)

| Engines | Events/s | Engine cores | CPU-seconds | Peak memory |
|---|---|---|---|---|
| 1 | 3.62M (2.99–3.83M) | 6.9 | 111 | 0.6 GB |
| 2 | 5.01M (5.00–5.03M) | 9.9 | 114 | 1.0 GB |
| 4 | 5.44M (5.40–5.54M) | 10.3 | 111 | 1.9 GB |
| 8 | 5.58M (5.58–5.75M) | 10.3 | 108 | 2.5 GB |

- Throughput levels off at about 10 engine cores; the rest of the 12 cores serve Kafka. A single engine already runs one goroutine per partition, so more instances mostly share the same cores.
- The total work stays about 110 CPU-seconds for the week at every instance count: extra instances cost almost no CPU, but memory grows with the number of instances.
- The first run after filling the topic is the slowest (2.99M versus 3.62M and 3.83M with one engine), in both sessions where E1 ran. The cause is not known; a discarded warm-up run would remove it from the median.

### E2: CPU budget of one engine (whole week, pre-filled)

| CPU limit | Events/s | Per core | Cores used | CPU-seconds |
|---|---|---|---|---|
| 0.5 | 0.38M | 0.76M | 0.50 | 78 |
| 1 | 0.73M | 0.73M | 1.01 | 82 |
| 2 | 1.49M | 0.75M | 1.99 | 77 |
| 4 | 2.56M | 0.64M | 3.96 | 84 |

- Throughput grows almost linearly with the CPU limit, at about 0.75M events/s per core up to 2 cores and somewhat less at 4. Memory stays at 0.5 GB.
- With a limit, the week costs 77–84 CPU-seconds; without one (E1) about 110. An unlimited engine spreads over about 7 cores and spends more CPU on the same work, probably in Go's scheduler and garbage collector; this is not verified.

### E3: replay speed (Monday, live)

| Speed | Events/s | Event latency p50 / p95 / p99 | Window latency p50 / p95 / p99 | Ingestion cores | Engine cores | Kafka cores |
|---|---|---|---|---|---|---|
| 1× | 397 | 10.9 / 13.0 / 13.2 ms | 7.8 / 15.1 / 15.5 ms | 0.01 | 0.02 | 0.48 |
| 10× | 3.6k | 8.8 / 12.9 / 13.2 ms | 11.8 / 15.3 / 15.6 ms | 0.05 | 0.05 | 0.52 |
| 60× | 18.5k | 8.1 / 12.9 / 14.3 ms | 8.9 / 14.4 / 15.4 ms | 0.15 | 0.12 | 0.53 |
| max | 235k (235–242k) | 9.7 / 15.1 / 17.3 ms | 10.9 / 15.8 / 18.0 ms | 1.13 | 0.69 | 0.85 |

- Latency stays below 20 ms at the 99th percentile at every speed, also when the pipeline runs as fast as it can.
- As fast as possible, the whole pipeline processes about 235k events/s, limited by ingestion (1.1 cores); the engine needs only 0.7 cores for it. This is the end-to-end throughput, against about 3.6M events/s for one engine on a full topic (E1).
- Kafka uses about 0.5 cores even at 1×, a fixed base cost. The sink stays below 0.02 cores.
- The median event latency falls from 10.9 ms at 1× to 8.1 ms at 60×, and the 95th percentile stays at 13 ms. This fits the producer's linger: franz-go waits up to 10 ms (`ProducerLinger`, default) before sending a partition's batch. At 1× a partition gets about 33 events/s, so nearly every event starts a batch and waits the full 10 ms; at higher speeds more events join a batch that is already waiting. A run with a shorter linger would confirm it; it has not been done.
- Window latency is about the same as event latency, since the closing watermark goes through the same producer. At 1× only a few windows close in the 10 minutes, so its percentiles vary between runs (median 4.1–14.5 ms).
