# breakout

breakout is the course project for the course CS-E4780 Scalable Systems and Data Management at Aalto University: a streaming system that replays one week of real stock-exchange tick data (the [DEBS 2022 Grand Challenge data set](https://doi.org/10.5281/zenodo.6382482)) and, for every symbol,

1. computes two exponential moving averages, EMA38 and EMA100, over 5-minute windows (Query 1), and
2. raises a **buy** advisory when EMA38 crosses above EMA100 and a **sell** advisory when it crosses below (Query 2).

Results are shown live in a web UI, and the system measures its own throughput and latency.

## Documentation

| Document | What it explains |
|---|---|
| [docs/architecture.md](docs/architecture.md) | The services, what flows between them, Kafka topics, storage, configuration, technology |
| [docs/data.md](docs/data.md) | The CSV files, the columns used, the data checks behind the constants, anomalies |
| [docs/windowing.md](docs/windowing.md) | How EMAs and advisories are computed: windows, watermarks, grace period, late events, edge cases |
| [docs/evaluation.md](docs/evaluation.md) | Metrics, latency definitions, tests and planned experiments |
| [docs/decisions.md](docs/decisions.md) | Every design decision with the alternatives considered, plus open questions |
| [docs/glossary.md](docs/glossary.md) | Short definitions of the terms used everywhere else |

## Status

| Step | State |
|---|---|
| Data checks (`tools/datacheck`) | Done for all 7 days |
| CSV parser (`internal/csvparse`) | Done: 0 allocations per line, all 7 days parse cleanly |
| Protobuf messages (`proto/`) | Next |
| Ingestion → Kafka, Docker Compose | Planned |
| Engine (windows, EMA, crossovers) | Planned |
| Sink + Postgres, API + UI | Planned |
| Metrics, dashboards, experiments | Planned |

## Getting started

Requirements: Go 1.27 or newer (see `go.mod`).

Download the seven daily CSV files from the data set link above into `data/`. They are about 4–5 GB each and are ignored by git.

Run the tests:

```bash
go test ./...
```

Parse a whole day file with the real parser and print how many lines were kept or dropped (the path must be absolute):

```bash
BREAKOUT_CSV=$PWD/data/debs2022-gc-trading-day-08-11-21.csv go test -run RealFile -v ./internal/csvparse
```

Run the data checks for the whole week (pass the files in date order; see [docs/data.md](docs/data.md#data-checks)):

```bash
go run ./tools/datacheck/grace data/debs2022-gc-trading-day-*.csv
```

```bash
go run ./tools/datacheck/order data/debs2022-gc-trading-day-*.csv
```

```bash
go run ./tools/datacheck/windows data/debs2022-gc-trading-day-*.csv
```

Build the report (Typst 0.14 or newer):

```bash
typst compile report/main.typ
```
