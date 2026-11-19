package main

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
	"github.com/onurminen/breakout/internal/engine"
	"github.com/onurminen/breakout/internal/stream"
)

const minuteMs = 60 * 1000

func tick(t *testing.T, message *ticksv1.TickMessage) *kgo.Record {
	t.Helper()
	value, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return &kgo.Record{Topic: stream.TicksTopic, Value: value}
}

func price(t *testing.T, symbol string, value float64, eventTimeMs int64) *kgo.Record {
	return tick(t, &ticksv1.TickMessage{Payload: &ticksv1.TickMessage_Price{Price: &ticksv1.PriceEvent{
		Symbol: symbol, SecType: ticksv1.SecType_SEC_TYPE_EQUITY, Price: value, EventTimeMs: eventTimeMs, IngestTimeNs: 1,
	}}})
}

func watermark(t *testing.T, eventTimeMs, ingestTimeNs int64) *kgo.Record {
	return tick(t, &ticksv1.TickMessage{Payload: &ticksv1.TickMessage_Watermark{Watermark: &ticksv1.Watermark{
		EventTimeMs: eventTimeMs, IngestTimeNs: ingestTimeNs,
	}}})
}

// TestHandle runs two symbols in one partition through the message loop. Both
// get the warm-up buy in their first window; B's second window is no crossover,
// A has no price there, and the final watermark closes A's last window.
func TestHandle(t *testing.T) {
	var published []*kgo.Record
	w := newWorker(1000, func(r *kgo.Record) { published = append(published, r) })
	w.now = func() time.Time { return time.Unix(0, 5000) }

	records := []*kgo.Record{
		price(t, "B", 100, 1*minuteMs),
		price(t, "A", 50, 2*minuteMs),
		watermark(t, 5*minuteMs+1000, 1000), // closes window 0 of A and B
		price(t, "B", 90, 6*minuteMs),
		price(t, "A", 20, 2*minuteMs),        // late
		watermark(t, 10*minuteMs+1000, 2000), // closes window 1 of B
		price(t, "A", 60, 11*minuteMs),
		watermark(t, math.MaxInt64, 3000), // closes window 2 of A
	}
	if err := w.handle(records); err != nil {
		t.Fatal(err)
	}
	if late := testutil.ToFloat64(lateEventsTotal); late != 1 {
		t.Errorf("late_events_total = %v, want 1", late)
	}

	type want struct {
		topic         string
		symbol        string
		windowStartMs int64
		side          resultsv1.Side // advisories only
		warmup        bool
		watermarkNs   int64
	}
	wants := []want{
		{stream.EmaResultsTopic, "A", 0, 0, false, 1000},
		{stream.AdvisoriesTopic, "A", 0, resultsv1.Side_SIDE_BUY, true, 1000},
		{stream.EmaResultsTopic, "B", 0, 0, false, 1000},
		{stream.AdvisoriesTopic, "B", 0, resultsv1.Side_SIDE_BUY, true, 1000},
		{stream.EmaResultsTopic, "B", engine.WindowMs, 0, false, 2000},
		{stream.EmaResultsTopic, "A", 2 * engine.WindowMs, 0, false, 3000},
	}
	if len(published) != len(wants) {
		t.Fatalf("published %d records, want %d", len(published), len(wants))
	}
	for i, want := range wants {
		r := published[i]
		if r.Topic != want.topic || string(r.Key) != want.symbol {
			t.Errorf("record %d: %s key %q, want %s key %q", i, r.Topic, r.Key, want.topic, want.symbol)
			continue
		}
		var symbol string
		var windowStartMs, watermarkNs, publishedNs int64
		var secType ticksv1.SecType
		if want.topic == stream.EmaResultsTopic {
			var result resultsv1.EmaResult
			if err := proto.Unmarshal(r.Value, &result); err != nil {
				t.Fatal(err)
			}
			symbol, secType, windowStartMs = result.Symbol, result.SecType, result.WindowStartMs
			watermarkNs, publishedNs = result.ClosingWatermarkIngestNs, result.PublishedNs
		} else {
			var advisory resultsv1.Advisory
			if err := proto.Unmarshal(r.Value, &advisory); err != nil {
				t.Fatal(err)
			}
			symbol, secType, windowStartMs = advisory.Symbol, advisory.SecType, advisory.WindowStartMs
			watermarkNs, publishedNs = advisory.ClosingWatermarkIngestNs, advisory.PublishedNs
			if advisory.Side != want.side || advisory.Warmup != want.warmup {
				t.Errorf("record %d: side %v warmup %v, want %v %v", i, advisory.Side, advisory.Warmup, want.side, want.warmup)
			}
		}
		if symbol != want.symbol || secType != ticksv1.SecType_SEC_TYPE_EQUITY || windowStartMs != want.windowStartMs ||
			watermarkNs != want.watermarkNs || publishedNs != 5000 {
			t.Errorf("record %d: symbol %s sec type %v window %d watermark ns %d published ns %d, want %+v and published ns 5000",
				i, symbol, secType, windowStartMs, watermarkNs, publishedNs, want)
		}
	}
}
