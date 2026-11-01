# Evaluation

What is measured and how correctness is tested.

## Metrics

Every Go service exposes `/metrics` for Prometheus; cAdvisor adds CPU and memory per container, including Kafka's. Dropped lines are counted per parser reason (`no_price`, `bad_price`, `bad_time`, `bad_line`).

## Latency

Both latencies compare two moments on the wall clock (all services share one machine), never event times.

- **Per-event latency** = time the engine processes an event − that event's `ingest_time_ns`.
- **Window-result latency** = a result's `published_ns` − `ingest_time_ns` of the watermark that closed its window. It excludes the window length and grace period, which are rules of the computation, not slowness.

## Correctness

1. **Unit tests** for the computation: crossovers, touching without crossing, empty and late windows, disorder inside a window, first window, zero price, midnight, near-equal EMAs.
2. **End-to-end run** on about a dozen symbols for one day through Docker Compose, to catch errors between services.

## Planned experiments

| # | Experiment | What varies | Reported |
|---|---|---|---|
| E1 | Parser throughput | `encoding/csv` vs `csvparse` | lines/s, allocations per line, CPU profile |
| E2 | Engine scale-out | 1, 2, 4, 8 engine instances | end-to-end events/s, CPU per container |
| E3 | CPU scaling | Docker Compose `cpus:` limits | events/s, latency |
| E4 | Replay speed | 1×, 10×, 60×, as fast as possible | per-event and window-result latency (p50, p95, p99) |
| E5 | Grace period | 0, 1, 10, 60 s | late events against window-result latency |
