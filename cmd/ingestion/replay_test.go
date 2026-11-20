package main

import (
	"math"
	"testing"
	"time"

	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/internal/stream"
)

type sent struct {
	partition int32
	key       string
	message   *ticksv1.TickMessage
}

func recordingReplay(speed float64) (*replay, *[]sent) {
	var messages []sent
	r := newReplay(func(partition int32, key string, message *ticksv1.TickMessage) {
		messages = append(messages, sent{partition, key, message})
	}, speed)
	return r, &messages
}

// watermarks returns the event time of each round of watermarks, checking that
// every round went to all partitions in order.
func watermarks(t *testing.T, messages []sent) []int64 {
	t.Helper()
	var rounds []int64
	for i := 0; i < len(messages); i++ {
		watermark := messages[i].message.GetWatermark()
		if watermark == nil {
			continue
		}
		for partition := range int32(stream.TicksPartitions) {
			m := messages[i+int(partition)]
			if m.partition != partition || m.message.GetWatermark().GetEventTimeMs() != watermark.EventTimeMs {
				t.Fatalf("watermark %d not sent to partitions 0–%d in order", watermark.EventTimeMs, stream.TicksPartitions-1)
			}
		}
		rounds = append(rounds, watermark.EventTimeMs)
		i += stream.TicksPartitions - 1
	}
	return rounds
}

func TestWatermarks(t *testing.T) {
	r, messages := recordingReplay(0)
	for _, eventTimeMs := range []int64{10_000, 10_500, 10_999, 11_000, 3_600, 11_200, 13_500} {
		r.onEvent(csvparse.Event{Symbol: "PX1.FR", SecType: 'I', Price: 1, EventTimeMs: eventTimeMs})
	}
	r.finish()

	got := watermarks(t, *messages)
	want := []int64{10_000, 11_000, 13_500, math.MaxInt64}
	if len(got) != len(want) {
		t.Fatalf("watermarks %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("watermarks %v, want %v", got, want)
		}
	}
}

func TestPriceEvent(t *testing.T) {
	r, messages := recordingReplay(0)
	r.onEvent(csvparse.Event{Symbol: "RDSA.NL", SecType: 'E', Price: 23.5, EventTimeMs: 42})

	first := (*messages)[0]
	price := first.message.GetPrice()
	if first.partition != stream.Partition("RDSA.NL") || first.key != "RDSA.NL" {
		t.Errorf("sent to partition %d with key %q", first.partition, first.key)
	}
	if price.GetSymbol() != "RDSA.NL" || price.GetSecType() != ticksv1.SecType_SEC_TYPE_EQUITY ||
		price.GetPrice() != 23.5 || price.GetEventTimeMs() != 42 || price.GetIngestTimeNs() == 0 {
		t.Errorf("price event %v", price)
	}
}

func TestPacing(t *testing.T) {
	r, _ := recordingReplay(10)
	wall := time.Unix(0, 0)
	var slept []time.Duration
	r.now = func() time.Time { return wall }
	r.sleep = func(d time.Duration) { slept = append(slept, d); wall = wall.Add(d) }

	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 100_000})   // pacing starts here
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 100_005})   // 0.5 ms ahead: no sleep
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 110_000})   // 10 s at 10× = 1 s
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 3_600_000}) // out of order but highest: paced on 3,600 s
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 1_000})     // behind: no sleep

	want := []time.Duration{time.Second, 349 * time.Second}
	if len(slept) != len(want) || slept[0] != want[0] || slept[1] != want[1] {
		t.Errorf("slept %v, want %v", slept, want)
	}
}

func TestPacingStartsAtPaceFrom(t *testing.T) {
	r, _ := recordingReplay(1)
	r.paceFromMs = 60_000
	wall := time.Unix(0, 0)
	var slept []time.Duration
	r.now = func() time.Time { return wall }
	r.sleep = func(d time.Duration) { slept = append(slept, d); wall = wall.Add(d) }

	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 0})      // before paceFromMs: as fast as possible
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 50_000}) // still before
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 61_000}) // pacing starts here
	r.onEvent(csvparse.Event{Symbol: "A", EventTimeMs: 63_000}) // 2 s at 1×

	if len(slept) != 1 || slept[0] != 2*time.Second {
		t.Errorf("slept %v, want [2s]", slept)
	}
}
