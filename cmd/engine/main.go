// Command engine reads price events and watermarks from the Kafka topic ticks,
// computes EMAs and crossovers with internal/engine, and publishes the results
// to ema-results and advisories (docs/architecture.md).
//
//	go run ./cmd/engine
//
// Several instances share the partitions of ticks as the consumer group
// "engine". Engine state is not saved and no offsets are committed: a partition
// an instance takes over is re-read from the beginning, which rebuilds its state.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/onurminen/breakout/internal/stream"
)

// From 100 µs to 60 s, each bucket about twice the previous.
var latencyBuckets = prometheus.ExponentialBucketsRange(100e-6, 60, 20)

var (
	recordsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "engine_records_total",
		Help: "Records read from ticks, price events and watermarks; with the topic's end offsets it gives the engine's lag.",
	})
	eventsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "engine_events_total",
		Help: "Price events processed.",
	})
	lateEventsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "late_events_total",
		Help: "Price events dropped because their window had already closed.",
	})
	resultsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "engine_results_total",
		Help: "EmaResults published, one per symbol and window with a price.",
	})
	advisoriesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "engine_advisories_total",
		Help: "Advisories published.",
	}, []string{"side", "warmup"})
	eventLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "engine_event_latency_seconds",
		Help:    "Wall-clock time from ingestion sending a price event to the engine processing it.",
		Buckets: latencyBuckets,
	})
	windowResultLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "engine_window_result_latency_seconds",
		Help:    "Wall-clock time from ingestion sending the watermark that closed a window to its result being published.",
		Buckets: latencyBuckets,
	})
)

func main() {
	brokers := getenv("KAFKA_BROKERS", "localhost:9094")
	metricsAddr := getenv("METRICS_ADDR", ":2112")
	graceMs, err := strconv.ParseInt(getenv("GRACE_MS", "1000"), 10, 64)
	if err != nil || graceMs < 0 {
		log.Fatalf("GRACE_MS must be an integer ≥ 0, got %q", os.Getenv("GRACE_MS"))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Fatal(http.ListenAndServe(metricsAddr, promhttp.Handler()))
	}()

	c := &consumer{graceMs: graceMs, workers: map[int32]*partitionWorker{}}
	client, err := stream.NewClient(brokers,
		kgo.ConsumerGroup("engine"),
		kgo.ConsumeTopics(stream.TicksTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(),
		kgo.OnPartitionsAssigned(c.assigned),
		kgo.OnPartitionsRevoked(c.revoked),
		kgo.OnPartitionsLost(c.revoked),
		kgo.ProducerBatchCompression(kgo.Lz4Compression()),
	)
	if err != nil {
		log.Fatal(err)
	}
	c.client = client

	c.poll(ctx)
	client.CloseAllowingRebalance() // revokes all partitions, which stops their workers
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
