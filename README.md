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
| Protobuf messages (`proto/`) | Done |
| Docker Compose infrastructure (`deploy/`) | Done: Kafka, Postgres, Prometheus, Grafana, cAdvisor |
| Engine core (`internal/engine`) | Done: windows, EMAs, crossovers; whole week checked with `tools/enginecheck` |
| Ingestion → Kafka (`cmd/ingestion`) | Done: events and watermarks to `ticks`, optional pacing |
| Engine service (`cmd/engine`) | Done: one goroutine per partition, results and advisories to Kafka, latency histograms |
| Sink (`cmd/sink`) | Done: results and advisories to Postgres, offsets committed after each insert |
| API (`cmd/api`) | Done: history and symbols over REST, KPIs, subscribed live results over WebSocket |
| UI (`ui/`) | Done: searchable watchlist, live price and EMA charts, advisories, KPIs |
| Metrics and dashboard (`deploy/`) | Done: every service scraped, consumer lag from `kafka-exporter`, Grafana dashboard provisioned |
| Experiments (`tools/experiments`) | Done: E1–E3 and the end-to-end check, one command each; results in `report/data/` and [evaluation.md](docs/evaluation.md#results) |

## Getting started

Requirements: Go 1.27 or newer (see `go.mod`).

For UI development, use Node.js 24 or newer. Start the API on port 8080, then run:

```bash
cd ui
npm ci
npm run dev
```

Open the Vite URL printed in the terminal. It proxies `/api` and `/ws` to the API. To check or build the UI, run `npm test` and `npm run build` in `ui/`. The production build writes `ui/dist`; the API serves it at http://localhost:8080. The Compose API image builds the UI automatically. After a `.proto` change, regenerate both Go and TypeScript types from the repository root with `ui/node_modules/.bin/buf generate` (after `npm ci` in `ui/`).

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

Run the whole week through the engine core without Kafka and count results, advisories and late events:

```bash
go run ./tools/enginecheck data/debs2022-gc-trading-day-*.csv
```

Start the infrastructure (Docker with Compose). The API is at http://localhost:8080, the Grafana dashboard at http://localhost:3000/d/breakout, and Prometheus at http://localhost:9090:

```bash
docker compose -f deploy/docker-compose.yml up -d
```

The API is also available from the host with `go run ./cmd/api`. Its REST routes are `GET /api/symbols`, `GET /api/history?symbol=PX1.FR&from=0&to=9999999999999` (epoch-millisecond bounds, start inclusive and end exclusive), and `GET /api/kpis`. `GET /ws` accepts `{"subscribe":["PX1.FR"]}` and `{"unsubscribe":["PX1.FR"]}`. It confirms subscriptions before sending `ema` and `advisory` messages for those symbols. The API serves the UI from `UI_DIR` once built; until then `/` returns 404. A production API image built by Compose includes the UI.

Start the engine, with as many instances as wanted, and the sink inside Compose. Start the engine before ingestion: an instance that takes over a partition later re-reads it from the beginning and publishes its results again ([architecture.md](docs/architecture.md#kafka-topics)):

```bash
docker compose -f deploy/docker-compose.yml up -d --scale engine=4 engine sink
```

Run the Postgres test against the Compose database (it skips itself without `DATABASE_URL`):

```bash
DATABASE_URL=postgres://breakout:breakout@localhost:5432/breakout go test ./internal/store
```

Replay one day into Kafka as fast as possible, from the host (files in date order; `REPLAY_SPEED=60` replays an hour per minute):

```bash
go run ./cmd/ingestion data/debs2022-gc-trading-day-08-11-21.csv
```

Or inside Compose, which replays the files listed under `command:` in the `ingestion` service:

```bash
docker compose -f deploy/docker-compose.yml up -d ingestion
```

Run an experiment from a clean stack (all containers are recreated); results go to `report/data/` ([evaluation.md](docs/evaluation.md#experiments)). `e2e` is the end-to-end check, which compares Postgres with `tools/enginecheck`:

```bash
go run ./tools/experiments e1    # also e2, e3, e2e
```

Regenerate the Go code after changing a `.proto` file ([buf](https://buf.build/docs/installation/)):

```bash
ui/node_modules/.bin/buf generate
```

Build the report (Typst 0.14 or newer):

```bash
typst compile report/main.typ
```
