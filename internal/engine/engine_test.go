package engine

import (
	"math"
	"testing"
)

const graceMs = 1000

// at returns an event time in ms from minutes and seconds after the first window starts.
func at(minutes, seconds float64) int64 { return int64((minutes*60 + seconds) * 1000) }

func near(a, b float64) bool { return math.Abs(a-b) <= 1e-12*math.Max(1, math.Abs(b)) }

func TestFirstWindowIsWarmupBuy(t *testing.T) {
	e := New(graceMs)
	e.OnPrice("A", 'I', 100, at(1, 0))
	if results := e.OnWatermark(at(5, 0.999)); results != nil {
		t.Fatalf("window closed during its grace period: %v", results)
	}
	results := e.OnWatermark(at(5, 1))
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Symbol != "A" || r.SecType != 'I' || r.WindowStartMs != 0 || r.LastPrice != 100 ||
		!near(r.EMA38, 100*2.0/39) || !near(r.EMA100, 100*2.0/101) || r.Advisory != Buy || !r.Warmup {
		t.Errorf("got %+v", r)
	}
}

func TestWindowPriceIsLatestEventTime(t *testing.T) {
	e := New(graceMs)
	e.OnPrice("A", 'E', 10, at(4, 0))
	e.OnPrice("A", 'E', 20, at(2, 0)) // older: ignored
	e.OnPrice("A", 'E', 30, at(4, 0)) // same time, arrived later: replaces
	if r := e.OnWatermark(at(10, 0)); len(r) != 1 || r[0].LastPrice != 30 {
		t.Errorf("got %+v, want one result with price 30", r)
	}
}

func TestGracePeriodAndLateEvents(t *testing.T) {
	e := New(graceMs)
	e.OnPrice("A", 'E', 10, at(4, 0))
	e.OnPrice("A", 'E', 11, at(5, 0.5)) // next window
	e.OnWatermark(at(5, 0.5))
	e.OnPrice("A", 'E', 12, at(4, 59.9)) // out of order, but its window is still open
	results := e.OnWatermark(at(5, 1))
	if len(results) != 1 || results[0].LastPrice != 12 {
		t.Fatalf("got %+v, want one result with price 12", results)
	}
	e.OnPrice("A", 'E', 13, at(4, 59.95)) // window closed: late
	if e.Late != 1 {
		t.Errorf("Late = %d, want 1", e.Late)
	}
	if r := e.OnWatermark(math.MaxInt64); len(r) != 1 || r[0].LastPrice != 11 {
		t.Errorf("final watermark: got %+v, want the open window with price 11", r)
	}
}

func TestEmptyWindowIsSkipped(t *testing.T) {
	e := New(graceMs)
	e.OnPrice("A", 'E', 100, at(1, 0))
	e.OnPrice("A", 'E', 200, at(11, 0)) // window 1 (5–10 min) has no price
	results := e.OnWatermark(math.MaxInt64)
	if len(results) != 2 || results[0].WindowStartMs != 0 || results[1].WindowStartMs != at(10, 0) {
		t.Fatalf("got %+v, want results for windows 0 and 2 only", results)
	}
	want38 := 100*alpha38*(1-alpha38) + 200*alpha38 // one EMA step per window with a price
	if !near(results[1].EMA38, want38) {
		t.Errorf("EMA38 = %v, want %v", results[1].EMA38, want38)
	}
}

func TestStateCarriesOverMidnight(t *testing.T) {
	const dayMs = 24 * 60 * 60 * 1000
	e := New(graceMs)
	e.OnPrice("A", 'E', 100, dayMs-at(1, 0))
	e.OnPrice("A", 'E', 200, dayMs+at(1, 0))
	results := e.OnWatermark(math.MaxInt64)
	if len(results) != 2 || results[1].Warmup || !near(results[1].EMA100, 100*alpha100*(1-alpha100)+200*alpha100) {
		t.Errorf("got %+v, want the second day to continue the first day's EMAs", results)
	}
}

func TestCrossovers(t *testing.T) {
	tests := []struct {
		name                    string
		previous38, previous100 float64
		price                   float64
		want                    Side
	}{
		{"bullish", 10, 11, 100, Buy},
		{"bearish", 10.1, 10, 1, Sell},
		{"stays above", 12, 10, 12, None},
		{"stays below", 10, 12, 10, None},
		{"touching, not crossing", 10, 10, 10, None},
		{"from equal to above", 10, 10, 20, Buy},
		{"from equal to below", 10, 10, 1, Sell},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := &symbolState{ema38: test.previous38, ema100: test.previous100, hasClosed: true}
			r := state.close(window{price: test.price})
			if r.Advisory != test.want || r.Warmup {
				t.Errorf("EMA38 %v, EMA100 %v: advisory %v warmup %v, want %v", r.EMA38, r.EMA100, r.Advisory, r.Warmup, test.want)
			}
		})
	}
}

func TestResultsOrderedByWindowThenSymbol(t *testing.T) {
	e := New(graceMs)
	e.OnPrice("B", 'E', 1, at(6, 0))
	e.OnPrice("B", 'E', 1, at(1, 0))
	e.OnPrice("A", 'E', 1, at(1, 0))
	var got []string
	for _, r := range e.OnWatermark(math.MaxInt64) {
		got = append(got, r.Symbol)
	}
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "B" {
		t.Errorf("got %v, want [A B B]", got)
	}
}

func BenchmarkOnPrice(b *testing.B) {
	e := New(graceMs)
	var eventTimeMs int64
	for b.Loop() {
		eventTimeMs += 10
		e.OnPrice("PX1.FR", 'I', 7000, eventTimeMs)
		if eventTimeMs%1000 == 0 {
			e.OnWatermark(eventTimeMs)
		}
	}
}
