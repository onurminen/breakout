package main

import (
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
	"github.com/onurminen/breakout/internal/engine"
	"github.com/onurminen/breakout/internal/stream"
)

// publishFunc hands one result record to the producer.
type publishFunc func(*kgo.Record)

// worker processes the records of one ticks partition with its own engine.
// One goroutine owns it, so the engine needs no locks.
type worker struct {
	core        *engine.Engine
	publish     publishFunc
	now         func() time.Time
	lateCounted int64 // part of core.Late already added to lateEventsTotal
}

func newWorker(graceMs int64, publish publishFunc) *worker {
	return &worker{core: engine.New(graceMs), publish: publish, now: time.Now}
}

// handle processes one batch of records in partition order.
func (w *worker) handle(records []*kgo.Record) error {
	var message ticksv1.TickMessage
	for _, record := range records {
		if err := proto.Unmarshal(record.Value, &message); err != nil {
			return fmt.Errorf("ticks/%d offset %d: %w", record.Partition, record.Offset, err)
		}
		switch payload := message.Payload.(type) {
		case *ticksv1.TickMessage_Price:
			price := payload.Price
			eventsTotal.Inc()
			eventLatency.Observe(secondsSince(price.IngestTimeNs, w.now()))
			w.core.OnPrice(price.Symbol, byte(price.SecType), price.Price, price.EventTimeMs)
		case *ticksv1.TickMessage_Watermark:
			w.publishResults(w.core.OnWatermark(payload.Watermark.EventTimeMs), payload.Watermark.IngestTimeNs)
		}
	}
	lateEventsTotal.Add(float64(w.core.Late - w.lateCounted))
	w.lateCounted = w.core.Late
	return nil
}

// publishResults sends each result to ema-results and, if it is a crossover, an
// advisory to advisories, keeping the engine's order.
func (w *worker) publishResults(results []engine.Result, watermarkIngestNs int64) {
	for _, r := range results {
		now := w.now()
		publishedNs := now.UnixNano()
		secType := ticksv1.SecType(r.SecType)
		w.publish(record(stream.EmaResultsTopic, r.Symbol, &resultsv1.EmaResult{
			Symbol:                   r.Symbol,
			SecType:                  secType,
			WindowStartMs:            r.WindowStartMs,
			LastPrice:                r.LastPrice,
			Ema38:                    r.EMA38,
			Ema100:                   r.EMA100,
			ClosingWatermarkIngestNs: watermarkIngestNs,
			PublishedNs:              publishedNs,
		}))
		resultsTotal.Inc()
		windowResultLatency.Observe(secondsSince(watermarkIngestNs, now))

		if r.Advisory == engine.None {
			continue
		}
		side, label := resultsv1.Side_SIDE_BUY, "buy"
		if r.Advisory == engine.Sell {
			side, label = resultsv1.Side_SIDE_SELL, "sell"
		}
		w.publish(record(stream.AdvisoriesTopic, r.Symbol, &resultsv1.Advisory{
			Symbol:                   r.Symbol,
			SecType:                  secType,
			WindowStartMs:            r.WindowStartMs,
			Side:                     side,
			Ema38:                    r.EMA38,
			Ema100:                   r.EMA100,
			Warmup:                   r.Warmup,
			ClosingWatermarkIngestNs: watermarkIngestNs,
			PublishedNs:              publishedNs,
		}))
		advisoriesTotal.With(prometheus.Labels{"side": label, "warmup": fmt.Sprint(r.Warmup)}).Inc()
	}
}

func record(topic, key string, message proto.Message) *kgo.Record {
	value, err := proto.Marshal(message)
	if err != nil {
		log.Fatal(err) // only invalid UTF-8 fails, and ingestion already marshaled the same symbol
	}
	return &kgo.Record{Topic: topic, Key: []byte(key), Value: value}
}

func secondsSince(startNs int64, now time.Time) float64 {
	return float64(now.UnixNano()-startNs) / 1e9
}
