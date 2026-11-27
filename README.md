# breakout

breakout is the course project for the course CS-E4780 Scalable Systems and Data Management at Aalto University: a streaming system that replays one week of real stock-exchange tick data (the [DEBS 2022 Grand Challenge data set](https://doi.org/10.5281/zenodo.6382482)) and, for every symbol,

1. computes two exponential moving averages, EMA38 and EMA100, over 5-minute windows (Query 1), and
2. raises a **buy** advisory when EMA38 crosses above EMA100 and a **sell** advisory when it crosses below (Query 2).

Results are shown live in a web UI, and the system measures its own throughput and latency.

## Try it out

You need Docker with Compose, git and curl, and the ports 3000, 5432, 8080, 9090 and 9094 free. Run every command from the repository root.

There are two options: **Monday only** (one file, 4.7 GB, replayed in about a minute) or **the whole week** (seven files, 25 GB, replayed in about 5 minutes). Steps 2 and 3 have a command for each; run only the one for your option.

**1. Clone the repository.**

```bash
git clone https://github.com/onurminen/breakout.git
cd breakout
```

**2. Download the data** into `data/`.

Monday only:

```bash
curl -fLo data/debs2022-gc-trading-day-08-11-21.csv --create-dirs https://zenodo.org/records/6382482/files/debs2022-gc-trading-day-08-11-21.csv
```

The whole week:

```bash
for day in 08 09 10 11 12 13 14; do curl -fLo data/debs2022-gc-trading-day-$day-11-21.csv --create-dirs https://zenodo.org/records/6382482/files/debs2022-gc-trading-day-$day-11-21.csv; done
```

**3. Start everything.** The first start builds the images, which takes a few minutes. Ingestion then replays the data as fast as possible.

Monday only:

```bash
docker compose -f deploy/docker-compose.yml --profile pipeline up -d
```

The whole week:

```bash
INGEST_FILES="/data/debs2022-gc-trading-day-08-11-21.csv /data/debs2022-gc-trading-day-09-11-21.csv /data/debs2022-gc-trading-day-10-11-21.csv /data/debs2022-gc-trading-day-11-11-21.csv /data/debs2022-gc-trading-day-12-11-21.csv /data/debs2022-gc-trading-day-13-11-21.csv /data/debs2022-gc-trading-day-14-11-21.csv" docker compose -f deploy/docker-compose.yml --profile pipeline up -d
```

**4. Look at the results.**

- UI: http://localhost:8080. Search for a symbol under "Symbols", for example `PX1.FR`, and select it to see its price, EMA38 and EMA100, and its advisories.
- Grafana dashboard: http://localhost:3000/d/breakout, showing throughput, latency, consumer lag, CPU and memory.

**5. Stop everything and delete all data.** To replay again or to switch options, run this and then step 3.

```bash
docker compose -f deploy/docker-compose.yml --profile pipeline down
```

### Experiments

Each experiment starts from a clean stack (all containers are recreated) and writes its results to `report/data/` ([evaluation.md](docs/evaluation.md#experiments)). `e2e` is the end-to-end check, which compares Postgres with `tools/enginecheck`.
Experiment 1 takes couple of minutes, experiment 2 15 minutes
and experiment 3 around 90 minutes.

```bash
go run ./tools/experiments e1
```

```bash
go run ./tools/experiments e2
```

```bash
go run ./tools/experiments e3
```

For a quick maximum-throughput check, run one full-speed E3 pass instead of
the complete three-run, four-speed benchmark:

```bash
go run ./tools/experiments e3-fast
```

It writes `e3-fast-summary.csv` and separate `e3-fast-*` time-series files.

## Documentation

| Document | What it explains |
|---|---|
| [docs/architecture.md](docs/architecture.md) | The services, what flows between them, Kafka topics, storage, configuration, technology |
| [docs/data.md](docs/data.md) | The CSV files, the columns used, the data checks behind the constants, anomalies |
| [docs/windowing.md](docs/windowing.md) | How EMAs and advisories are computed: windows, watermarks, grace period, late events, edge cases |
| [docs/evaluation.md](docs/evaluation.md) | Metrics, latency definitions, tests, experiments and their results |
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

## Development

Requirements: Go 1.27.3 or newer (see `go.mod`); Node.js 24 or newer for the UI and Protobuf generation; Typst 0.14 or newer for the report.

The data checks, `tools/enginecheck` and the experiments need the whole week ([Try it out](#try-it-out), step 2). The CSV files in `data/` are ignored by git.

### Tests and checks

```bash
go test ./...
```

The Postgres test runs against the Compose database and skips itself without `DATABASE_URL`:

```bash
DATABASE_URL=postgres://breakout:breakout@localhost:5432/breakout go test ./internal/store
```

The data checks for the whole week, with the files in date order ([docs/data.md](docs/data.md#data-checks)):

```bash
go run ./tools/datacheck/pricedrops data/debs2022-gc-trading-day-*.csv
go run ./tools/datacheck/grace data/debs2022-gc-trading-day-*.csv
go run ./tools/datacheck/order data/debs2022-gc-trading-day-*.csv
go run ./tools/datacheck/windows data/debs2022-gc-trading-day-*.csv
go run ./tools/datacheck/tradingdate data/debs2022-gc-trading-day-*.csv
```

The whole week through the engine core without Kafka, counting results, advisories and late events:

```bash
go run ./tools/enginecheck data/debs2022-gc-trading-day-*.csv
```

### Services one at a time

Without `--profile pipeline`, Compose starts only the infrastructure and the API: Kafka (port 9094 from the host), Postgres, Prometheus (http://localhost:9090), Grafana, cAdvisor, kafka-exporter and the API with the UI (http://localhost:8080).

```bash
docker compose -f deploy/docker-compose.yml up -d
```

Start the engine, with any number of instances, and the sink. Start the engine before ingestion: an instance that takes over a partition later re-reads it from the beginning and publishes its results again ([architecture.md](docs/architecture.md#kafka-topics)).

```bash
docker compose -f deploy/docker-compose.yml up -d --scale engine=4 engine sink
```

Replay the files listed under `command:` in the `ingestion` service, or replay from the host (files in date order; `REPLAY_SPEED=60` replays an hour per minute):

```bash
docker compose -f deploy/docker-compose.yml up -d ingestion
go run ./cmd/ingestion data/debs2022-gc-trading-day-08-11-21.csv
```

The API also runs from the host with `go run ./cmd/api`. Its REST routes are `GET /api/symbols`, `GET /api/history?symbol=PX1.FR&from=0&to=9999999999999` (epoch-millisecond bounds, start inclusive and end exclusive), and `GET /api/kpis`. `GET /ws` accepts `{"subscribe":["PX1.FR"]}` and `{"unsubscribe":["PX1.FR"]}`, confirms subscriptions, and then sends `ema` and `advisory` messages for those symbols. The API serves the UI from `UI_DIR`; the Compose image includes the built UI.

### UI

With the API running on port 8080:

```bash
cd ui
npm ci
npm run dev
```

Open the Vite URL printed in the terminal; it proxies `/api` and `/ws` to the API. `npm test` runs the UI tests and `npm run build` writes the production build to `ui/dist`.

### Protobuf

After changing a `.proto` file, regenerate the Go and TypeScript types. `npm ci` in `ui/` installs the project-local [Buf CLI](https://buf.build/docs/cli/installation/):

```bash
ui/node_modules/.bin/buf generate
```

### Report

Build the report (Typst 0.14 or newer) with the fonts in `report/fonts/`:

```bash
typst compile --font-path report/fonts report/main.typ
```
