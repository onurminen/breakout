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

## Correctness

1. **Unit tests** for the computation: crossovers, touching without crossing, empty and late windows, disorder inside a window, first window, zero price, midnight, near-equal EMAs.
2. **Whole week through the engine core** without Kafka (`tools/enginecheck`): 1,855,645 results, exactly the number of windows with a price from the [windows check](data.md#empty-windows-windows), and 5,506 late events, exactly Wednesday's 01:00 events.
3. **End-to-end check** through the whole Compose pipeline (`go run ./tools/experiments e2e`): ingestion replays the whole week for twelve symbols (`SYMBOLS`), two equities and two indices from each exchange. Once the sink has stored every result, `tools/enginecheck -symbols … -db …` computes the same symbols without Kafka and compares both Postgres tables with its results row by row, comparing the doubles exactly. The whole week is needed because the first real crossovers come on Wednesday, after the warm-up; it also covers late events and EMA state across midnight.

## Experiments

Each experiment is one command, `go run ./tools/experiments e1|e2|e3`. It starts from an empty stack, runs every setting three times, and writes to `report/data/`: `<experiment>-summary.csv` with one row per run, and one time-series file per run (`seconds,series,value`).

| # | Experiment | What varies | Input | Reported |
|---|---|---|---|---|
| E1 | Engine scale-out | 1, 2, 4, 8 engine instances, no CPU limit | Whole week, pre-filled | events/s, CPU seconds and cores, peak memory of the engines |
| E2 | CPU budget of one engine | `cpus:` 0.5, 1, 2, 4 for a single engine | Whole week, pre-filled | same as E1 |
| E3 | Replay speed | 1×, 10×, 60×, as fast as possible; one engine and the sink | Monday live, paced runs from 09:00 for 10 minutes | events/s, both latencies (p50, p95, p99), cores per container |

**Pre-filled topic (E1, E2).** Ingestion alone produces about 220k records/s, while one engine reads about 3 million records/s from a full topic, so in a live run the engines would only wait for ingestion. E1 and E2 therefore fill `ticks` with the whole week first and then start the engines on the full topic; each run reads it from the start, since the engine commits no offsets. Throughput is measured between the first scrape (every 250 ms) in which the engines have read anything and the first in which they have read the whole topic, so container startup and joining the group are left out. One engine reads Monday alone in about 5 s, too short to time precisely; the whole week takes several times longer. A run fails if the engines read more records than the topic holds, which would mean a partition was re-read after a rebalance; Kafka's initial rebalance delay (3 s) lets engines started together join before the partitions are assigned. Live end-to-end throughput, limited by ingestion, is E3's "as fast as possible" row.

**Paced runs (E3).** `REPLAY_FROM` makes ingestion replay the night before 09:00 as fast as possible and pace from there. Latencies and throughput are taken over the 10 minutes after pacing starts, from the histograms' increase over exactly that window.

Further settings that may be worth running are listed with the E2 decision in [decisions.md](decisions.md#evaluation).
