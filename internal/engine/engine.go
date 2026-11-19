// Package engine computes EMA38/EMA100 per symbol over 5-minute event-time
// windows and detects crossovers (docs/windowing.md). It only computes: the
// caller feeds it prices and watermarks and handles Kafka, clocks and encoding.
package engine

import (
	"cmp"
	"math"
	"slices"
	"strings"
)

// WindowMs is the length of one tumbling window.
const WindowMs = 5 * 60 * 1000

// Smoothing factors 2/(1+j) for j = 38 and j = 100.
const (
	alpha38  = 2.0 / (1 + 38)
	alpha100 = 2.0 / (1 + 100)
)

// Side is the kind of advisory a closed window produced, if any.
type Side int8

const (
	None Side = iota
	Buy
	Sell
)

// Result is one closed window that had at least one price.
type Result struct {
	Symbol        string
	SecType       byte // passed through unchanged; the engine does not interpret it
	WindowStartMs int64
	LastPrice     float64
	EMA38         float64
	EMA100        float64
	Advisory      Side
	Warmup        bool // the artificial buy of a symbol's first window
}

type window struct {
	index       int64   // event time / WindowMs
	price       float64 // price of the event with the highest event time so far
	eventTimeMs int64
}

type symbolState struct {
	symbol  string
	secType byte
	open    []window // ascending by index; at most two windows are open at a time, since we
	//  have a grace period of 1s
	ema38, ema100 float64 // after the last closed window
	hasClosed     bool
}

// Engine holds the state of the symbols of one partition. It is not safe for
// concurrent use; one goroutine owns it.
type Engine struct {
	graceMs     int64
	watermark   int64 // highest watermark received
	symbols     map[string]*symbolState
	active      map[string]*symbolState // Symbols with at least one open window
	nextCloseMs int64                   // Earliest  watermark that closes one of the active windows

	Late int64 // prices dropped because their window had already closed
}

// New returns an engine whose windows close when the watermark reaches their end plus graceMs.
func New(graceMs int64) *Engine {
	return &Engine{
		graceMs:     graceMs,
		watermark:   math.MinInt64,
		symbols:     map[string]*symbolState{},
		active:      map[string]*symbolState{},
		nextCloseMs: math.MaxInt64,
	}
}

func (e *Engine) closesAtMs(index int64) int64 { return (index+1)*WindowMs + e.graceMs }

// OnPrice adds a price to its window, or counts it as late if that window has closed.
func (e *Engine) OnPrice(symbol string, secType byte, price float64, eventTimeMs int64) {
	index := eventTimeMs / WindowMs
	closesAt := e.closesAtMs(index)
	if closesAt <= e.watermark {
		e.Late++
		return
	}

	state := e.symbols[symbol]
	if state == nil {
		state = &symbolState{symbol: symbol, secType: secType}
		e.symbols[symbol] = state
	}
	e.active[symbol] = state
	e.nextCloseMs = min(e.nextCloseMs, closesAt)

	// Search from the end: almost every price belongs to the newest window.
	position := len(state.open)
	for position > 0 && state.open[position-1].index >= index {
		position--
	}
	if position < len(state.open) && state.open[position].index == index {
		if eventTimeMs >= state.open[position].eventTimeMs {
			state.open[position].price, state.open[position].eventTimeMs = price, eventTimeMs
		}
		return
	}
	state.open = slices.Insert(state.open, position, window{index: index, price: price, eventTimeMs: eventTimeMs})
}

// OnWatermark closes every window that ends at or before watermarkMs - grace and
// returns their results, ordered by window start and then symbol.
func (e *Engine) OnWatermark(watermarkMs int64) []Result {
	e.watermark = max(e.watermark, watermarkMs)
	if e.watermark < e.nextCloseMs {
		return nil
	}

	var results []Result
	e.nextCloseMs = math.MaxInt64
	for symbol, state := range e.active {
		closed := 0
		for closed < len(state.open) && e.closesAtMs(state.open[closed].index) <= e.watermark {
			results = append(results, state.close(state.open[closed]))
			closed++
		}
		state.open = slices.Delete(state.open, 0, closed)
		if len(state.open) == 0 {
			delete(e.active, symbol)
		} else {
			e.nextCloseMs = min(e.nextCloseMs, e.closesAtMs(state.open[0].index))
		}
	}
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Or(cmp.Compare(a.WindowStartMs, b.WindowStartMs), strings.Compare(a.Symbol, b.Symbol))
	})
	return results
}

// close applies one EMA step with the window's price and checks for a crossover
// against the previous closed window. Empty windows never get here, so
// "previous" is the last window that had a price.
func (s *symbolState) close(w window) Result {
	previous38, previous100 := s.ema38, s.ema100
	s.ema38 = w.price*alpha38 + previous38*(1-alpha38)
	s.ema100 = w.price*alpha100 + previous100*(1-alpha100)

	advisory := None
	if s.ema38 > s.ema100 && previous38 <= previous100 {
		advisory = Buy
	} else if s.ema38 < s.ema100 && previous38 >= previous100 {
		advisory = Sell
	}
	first := !s.hasClosed
	s.hasClosed = true

	return Result{
		Symbol:        s.symbol,
		SecType:       s.secType,
		WindowStartMs: w.index * WindowMs,
		LastPrice:     w.price,
		EMA38:         s.ema38,
		EMA100:        s.ema100,
		Advisory:      advisory,
		Warmup:        first && advisory == Buy,
	}
}
