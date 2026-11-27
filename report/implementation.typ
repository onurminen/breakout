= Implementation <sec:implementation>

This section describes how the system architecture outlined in
@sec:architecture was implemented. The purpose of this section is to
introduce the central programming techniques that were used during
implementation.

== Backend <subsec:backend>

All backend services are written in Go. We chose Go since it is fast,
compiles to a single static binary, and makes concurrency simple with
goroutines. Furthermore, none of us had used Go before, and learning it
was one of our goals for this project. The services share a single Go
module. Each service has its own binary in `cmd/`, and code that is
shared between services, such as the CSV parser, lives in `internal/`.

The engine core in `internal/engine` contains the logic for windows,
EMAs and crossovers, but does not depend on Kafka. This allows us to
test the computations with plain unit tests, and to run the whole week
of data through the engine core without Kafka, as described in
@subsec:correctness. Each goroutine of an engine owns its own instance
of the engine core, and never shares its state with other goroutines.
Thus the engine needs no locks.

Parsing CSV lines dominates the CPU usage of ingestion. Therefore we
wrote our own CSV parser instead of using Go's `encoding/csv` package,
which allocates memory for every line. Our parser splits a line in
place, only reads the columns that we need, and caches parsed dates and
symbol strings. As a result, it allocates no memory per line, and one
CPU core parses approximately 870,000 events per second.

== Kafka <subsec:kafka>

Kafka is the only boundary between services: services never call each
other directly, but only read from and write to topics. We use the
`franz-go` client library. Automatic topic creation is disabled, and the
three topics are created by a separate container when the system
starts. A typo in a topic name thus fails instead of silently creating
a new topic.

All messages are serialized with Protobuf. The message schemas are
defined in `proto/`, and the `buf` tool generates both Go and
TypeScript types from them. This way the services and the UI always
agree on the message format. We chose Protobuf over JSON, since its
messages are smaller and faster to encode and decode. Price events and
watermarks are both wrapped in a single `TickMessage` type, so that
they can be sent to the same partition in order.

== Sink <subsec:sink-implementation>

The sink stores results in PostgreSQL, using the `pgx` driver. Both
tables, `ema_results` and `advisories`, have the primary key
`(symbol, window_start_ms)`. The sink reads both result topics in
batches of up to 10,000 records, and writes each batch in one
transaction with an `INSERT ... ON CONFLICT DO NOTHING` statement. Unlike
the engine, the sink commits its Kafka offset, but only after the
transaction succeeds. If the sink crashes, it continues from the last
committed offset and writes the last batch again. The primary key turns
these repeated rows into no-ops, so no duplicates are stored.

== UI <subsec:ui-implementation>

The UI is written in TypeScript with React, and built with Vite. Price
and EMA charts with buy and sell markers are drawn with the
`lightweight-charts` library. The UI decodes messages from the API with
the TypeScript types that `buf` generates, so no message types are
written by hand. The API is written in Go with the standard library's
`net/http` package, and serves both the API endpoints and the built UI
files. Live results are sent over a WebSocket connection, using the
`coder/websocket` library. The UI keeps the results of each symbol in a
map keyed by window start, so a result that arrives both live and in
the history is only stored once. Furthermore, the UI shows performance
figures such as throughput and latency, which the API reads from
Prometheus.

== Deployment <subsec:deployment>

All services run on a single machine with Docker Compose. The
ingestion, engine and sink services are built from the same Dockerfile
into small images, and the API is built together with the UI. Services
are configured with environment variables, such as the grace period of
the engine or the replay speed of ingestion. Engines are scaled with
`docker compose up --scale engine=N`, which starts $N$ replicas that
join the same Kafka consumer group. Kafka runs as a single broker
without volumes, so `docker compose down` deletes all topic data. This
gives each experiment in @sec:performance a clean start.

== Metrics <subsec:metrics>

Each Go service exposes its metrics with the Prometheus client library
at a `/metrics` endpoint. Examples are counters for processed events,
results and late events, as well as histograms for latency. To measure
latency, ingestion stamps each price event and watermark with its
wall-clock send time. The engine records the time from sending a price
event to processing it, and the time from sending the watermark that
closes a window to publishing the window's result to Kafka.
Prometheus collects the metrics of all services. In addition, cAdvisor
measures the CPU and memory usage of each container, and `kafka-exporter`
measures how far consumers lag behind. A Grafana dashboard visualizes all
metrics. The results of these measurements are presented in
@sec:performance.
