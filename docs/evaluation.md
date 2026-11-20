# Evaluation

What is measured and how correctness is tested.

## Metrics

Every Go service exposes `/metrics` for Prometheus; cAdvisor adds CPU and memory per container, including Kafka's. Dropped lines are counted per parser reason (`no_price`, `bad_price`, `bad_time`, `bad_line`). The engine counts `engine_events_total`, `late_events_total`, `engine_results_total` and `engine_advisories_total{side,warmup}`. The sink counts new rows per table (`sink_rows_total{table}`) and records batch sizes (`sink_batch_rows`) and insert times (`sink_insert_seconds`).

## Latency

Both latencies compare two moments on the wall clock (all services share one machine), never event times.

- **Per-event latency** = time the engine processes an event − that event's `ingest_time_ns` (`engine_event_latency_seconds`).
- **Window-result latency** = a result's `published_ns` − `ingest_time_ns` of the watermark that closed its window (`engine_window_result_latency_seconds`). It excludes the window length and grace period, which are rules of the computation, not slowness. `published_ns` is when the result is handed to the Kafka producer.

## Correctness

1. **Unit tests** for the computation: crossovers, touching without crossing, empty and late windows, disorder inside a window, first window, zero price, midnight, near-equal EMAs.
2. **Whole week through the engine core** without Kafka (`tools/enginecheck`): 1,855,645 results, exactly the number of windows with a price from the [windows check](data.md#empty-windows-windows), and 5,506 late events, exactly Wednesday's 01:00 events.
3. **End-to-end run** on about a dozen symbols for one day through Docker Compose, to catch errors between services.

## Planned experiments

| # | Experiment | What varies | Reported |
|---|---|---|---|
| E1 | Engine scale-out | 1, 2, 4, 8 engine instances | end-to-end events/s, CPU per container |
| E2 | CPU scaling | Docker Compose `cpus:` limits | events/s, latency |
| E3 | Replay speed | 1×, 10×, 60×, as fast as possible | per-event and window-result latency (p50, p95, p99) |
