// Command sink copies EMA results and advisories from Kafka into Postgres
// (docs/architecture.md#storage).
//
//	go run ./cmd/sink
//
// Each polled batch is written with one statement per table, and only then is
// the batch's offset committed. After a crash the uncommitted batch is written
// again, and the primary key turns the repeated rows into no-ops.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	"github.com/onurminen/breakout/internal/store"
	"github.com/onurminen/breakout/internal/stream"
)

// maxBatchRecords bounds one batch, and so one INSERT statement.
const maxBatchRecords = 10_000

var (
	rowsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sink_rows_total",
		Help: "Rows inserted, not counting repeats the primary key skipped.",
	}, []string{"table"})
	batchRows = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sink_batch_rows",
		Help:    "Records per polled batch.",
		Buckets: prometheus.ExponentialBuckets(1, 4, 8), // 1 to 16384
	})
	insertSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sink_insert_seconds",
		Help:    "Time to write one batch to Postgres, both tables.",
		Buckets: prometheus.ExponentialBucketsRange(1e-3, 10, 14),
	})
)

func main() {
	brokers := getenv("KAFKA_BROKERS", "localhost:9094")
	databaseURL := getenv("DATABASE_URL", "postgres://breakout:breakout@localhost:5432/breakout")
	metricsAddr := getenv("METRICS_ADDR", ":2112")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Fatal(http.ListenAndServe(metricsAddr, promhttp.Handler()))
	}()

	db, err := store.Connect(databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	client, err := stream.NewClient(brokers,
		kgo.ConsumerGroup("sink"),
		kgo.ConsumeTopics(stream.EmaResultsTopic, stream.AdvisoriesTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
		kgo.BlockRebalanceOnPoll(), // no rebalance between a poll and its commit
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.CloseAllowingRebalance()

	for {
		fetches := client.PollRecords(ctx, maxBatchRecords)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			log.Fatalf("fetch %s/%d: %v", topic, partition, err)
		})
		results, advisories, err := decode(fetches.Records())
		if err != nil {
			log.Fatal(err)
		}
		if !writeWithRetry(ctx, db, results, advisories) {
			return
		}
		if err := client.CommitUncommittedOffsets(ctx); err != nil {
			// Not fatal: a later commit covers this batch, and until then a restart only repeats it.
			log.Printf("commit offsets: %v", err)
		}
		client.AllowRebalance()
	}
}

func decode(records []*kgo.Record) ([]*resultsv1.EmaResult, []*resultsv1.Advisory, error) {
	var results []*resultsv1.EmaResult
	var advisories []*resultsv1.Advisory
	for _, record := range records {
		var err error
		switch record.Topic {
		case stream.EmaResultsTopic:
			result := &resultsv1.EmaResult{}
			err = proto.Unmarshal(record.Value, result)
			results = append(results, result)
		case stream.AdvisoriesTopic:
			advisory := &resultsv1.Advisory{}
			err = proto.Unmarshal(record.Value, advisory)
			advisories = append(advisories, advisory)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s/%d offset %d: %w", record.Topic, record.Partition, record.Offset, err)
		}
	}
	return results, advisories, nil
}

// writeWithRetry writes both tables, retrying with backoff while Postgres is
// unavailable. It returns false only if the sink is stopped first.
func writeWithRetry(ctx context.Context, db *store.Store, results []*resultsv1.EmaResult, advisories []*resultsv1.Advisory) bool {
	backoff := 100 * time.Millisecond
	for {
		err := write(ctx, db, results, advisories)
		if err == nil {
			return true
		}
		log.Printf("write batch, retrying in %s: %v", backoff, err)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(backoff):
		}
		backoff = min(2*backoff, 10*time.Second)
	}
}

func write(ctx context.Context, db *store.Store, results []*resultsv1.EmaResult, advisories []*resultsv1.Advisory) error {
	start := time.Now()
	newResults, newAdvisories, err := db.Insert(ctx, results, advisories)
	if err != nil {
		return err
	}
	insertSeconds.Observe(time.Since(start).Seconds())
	batchRows.Observe(float64(len(results) + len(advisories)))
	rowsTotal.WithLabelValues("ema_results").Add(float64(newResults))
	rowsTotal.WithLabelValues("advisories").Add(float64(newAdvisories))
	return nil
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
