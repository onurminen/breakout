// Command api serves result history and live, symbol-filtered updates to the UI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	"github.com/onurminen/breakout/internal/store"
	"github.com/onurminen/breakout/internal/stream"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Connect(getenv("DATABASE_URL", "postgres://breakout:breakout@localhost:5432/breakout"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	client, err := stream.NewClient(getenv("KAFKA_BROKERS", "localhost:9094"),
		kgo.ConsumeTopics(stream.EmaResultsTopic, stream.AdvisoriesTopic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	a := &api{
		store: db, hub: newHub(), uiDir: getenv("UI_DIR", "ui/dist"),
		prometheusURL: getenv("PROMETHEUS_URL", "http://localhost:9090"),
		upstream:      &http.Client{Timeout: 3 * time.Second},
	}
	server := &http.Server{Addr: getenv("HTTP_ADDR", ":8080"), Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second}
	metrics := &http.Server{Addr: getenv("METRICS_ADDR", ":2112"), Handler: promhttp.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go serve(server)
	go serve(metrics)
	go func() {
		if err := consume(ctx, client, a.hub); err != nil && ctx.Err() == nil {
			log.Fatalf("consume results: %v", err)
		}
	}()

	<-ctx.Done()
	a.hub.closeAll()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	_ = metrics.Shutdown(shutdownCtx)
}

func serve(server *http.Server) {
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen on %s: %v", server.Addr, err)
	}
}

func consume(ctx context.Context, client *kgo.Client, hub *hub) error {
	for {
		fetches := client.PollFetches(ctx)
		if ctx.Err() != nil || fetches.IsClientClosed() {
			return nil
		}
		var fetchErr error
		fetches.EachError(func(topic string, partition int32, err error) {
			if fetchErr == nil {
				fetchErr = fmt.Errorf("%s/%d: %w", topic, partition, err)
			}
		})
		if fetchErr != nil {
			return fetchErr
		}
		for _, record := range fetches.Records() {
			kind, symbol, data, err := encodeRecord(record)
			if err != nil {
				return fmt.Errorf("%s/%d offset %d: %w", record.Topic, record.Partition, record.Offset, err)
			}
			hub.publish(symbol, kind, data)
		}
	}
}

func encodeRecord(record *kgo.Record) (kind, symbol string, data []byte, err error) {
	var message proto.Message
	switch record.Topic {
	case stream.EmaResultsTopic:
		result := &resultsv1.EmaResult{}
		message, kind = result, "ema"
		if err = proto.Unmarshal(record.Value, result); err == nil {
			symbol = result.Symbol
		}
	case stream.AdvisoriesTopic:
		advisory := &resultsv1.Advisory{}
		message, kind = advisory, "advisory"
		if err = proto.Unmarshal(record.Value, advisory); err == nil {
			symbol = advisory.Symbol
		}
	default:
		return "", "", nil, fmt.Errorf("unexpected topic %s", record.Topic)
	}
	if err != nil {
		return "", "", nil, err
	}
	payload, err := protojson.Marshal(message)
	if err != nil {
		return "", "", nil, err
	}
	data, err = json.Marshal(struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}{kind, payload})
	return kind, symbol, data, err
}

func getenv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
