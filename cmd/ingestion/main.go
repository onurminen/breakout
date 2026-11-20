// Command ingestion replays the day CSV files, given in date order, into the
// Kafka topic ticks: valid price events and watermarks (docs/architecture.md).
//
//	go run ./cmd/ingestion data/debs2022-gc-trading-day-*.csv
//
// After the last file it keeps serving /metrics until stopped, so Prometheus
// can scrape the final counts.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/internal/stream"
)

var reasonLabels = [...]string{
	csvparse.OK:       "ok",
	csvparse.Skip:     "skip",
	csvparse.NoPrice:  "no_price",
	csvparse.BadPrice: "bad_price",
	csvparse.BadTime:  "bad_time",
	csvparse.BadLine:  "bad_line",
}

var (
	linesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ingest_lines_total",
		Help: "CSV lines read, by parser reason.",
	}, []string{"reason"})
	watermarksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ingest_watermarks_total",
		Help: "Watermark messages sent, one per partition per watermark.",
	})
	eventTimeMs = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ingest_event_time_ms",
		Help: "Highest event time covered by a watermark, to show replay progress.",
	})
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s DAY.csv [DAY.csv ...]\n", os.Args[0])
		os.Exit(2)
	}
	brokers := getenv("KAFKA_BROKERS", "localhost:9094")
	metricsAddr := getenv("METRICS_ADDR", ":2112")
	speed, err := strconv.ParseFloat(getenv("REPLAY_SPEED", "0"), 64)
	if err != nil || speed < 0 {
		log.Fatalf("REPLAY_SPEED must be a number ≥ 0, got %q", os.Getenv("REPLAY_SPEED"))
	}
	paceFromMs := int64(math.MinInt64)
	if from := os.Getenv("REPLAY_FROM"); from != "" {
		t, err := time.Parse(time.RFC3339, from)
		if err != nil {
			log.Fatalf("REPLAY_FROM must be an RFC 3339 time such as 2021-11-08T09:00:00Z, got %q", from)
		}
		paceFromMs = t.UnixMilli()
	}
	var symbols map[string]bool // nil: all symbols
	if list := os.Getenv("SYMBOLS"); list != "" {
		symbols = map[string]bool{}
		for _, symbol := range strings.Split(list, ",") {
			symbols[symbol] = true
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Fatal(http.ListenAndServe(metricsAddr, promhttp.Handler()))
	}()

	client, err := stream.NewClient(brokers,
		kgo.RecordPartitioner(kgo.ManualPartitioner()),
		kgo.ProducerBatchCompression(kgo.Lz4Compression()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	r := newReplay(func(partition int32, key string, message *ticksv1.TickMessage) {
		if watermark := message.GetWatermark(); watermark != nil {
			watermarksTotal.Inc()
			if watermark.EventTimeMs != math.MaxInt64 { // the final watermark would flatten the graph
				eventTimeMs.Set(float64(watermark.EventTimeMs))
			}
		}
		produce(ctx, client, partition, key, message)
	}, speed)
	r.paceFromMs = paceFromMs

	parser := csvparse.New()
	var lines [len(reasonLabels)]prometheus.Counter
	for reason, label := range reasonLabels {
		lines[reason] = linesTotal.WithLabelValues(label)
	}
	for _, path := range os.Args[1:] {
		start := time.Now()
		if err := replayFile(path, parser, r, symbols, &lines); err != nil {
			log.Fatal(err)
		}
		log.Printf("replayed %s in %s", path, time.Since(start).Round(time.Millisecond))
	}
	r.finish()
	if err := client.Flush(ctx); err != nil {
		log.Fatal(err)
	}

	log.Printf("done; serving metrics on %s until stopped", metricsAddr)
	<-ctx.Done()
}

func replayFile(path string, parser *csvparse.Parser, r *replay, symbols map[string]bool, lines *[len(reasonLabels)]prometheus.Counter) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		line, err := reader.ReadSlice('\n')
		if len(line) > 0 {
			event, reason := parser.Parse(line)
			lines[reason].Inc()
			if reason == csvparse.OK && (symbols == nil || symbols[event.Symbol]) {
				r.onEvent(event)
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
}

// produce sends asynchronously; a failed send stops ingestion, since a gap in a
// partition would silently change results.
func produce(ctx context.Context, client *kgo.Client, partition int32, key string, message *ticksv1.TickMessage) {
	value, err := proto.Marshal(message)
	if err != nil {
		log.Fatal(err)
	}
	record := &kgo.Record{Topic: stream.TicksTopic, Partition: partition, Value: value}
	if key != "" {
		record.Key = []byte(key)
	}
	client.Produce(ctx, record, func(record *kgo.Record, err error) {
		if err != nil {
			log.Fatalf("produce to %s/%d: %v", record.Topic, record.Partition, err)
		}
	})
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
