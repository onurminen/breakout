// Command enginecheck runs day files through the engine core without Kafka and
// prints how many results, advisories and late events it produced. Watermarks
// are sent like ingestion sends them: whenever the highest event time seen has
// advanced by a second, and a final one at the end.
//
//	go run ./tools/enginecheck data/debs2022-gc-trading-day-*.csv
package main

import (
	"fmt"
	"math"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/internal/engine"
	"github.com/onurminen/breakout/tools/datacheck"
)

const watermarkIntervalMs = 1000

type counts struct{ results, buys, sells, warmups, late int64 }

func (c *counts) add(results []engine.Result) {
	c.results += int64(len(results))
	for _, r := range results {
		switch r.Advisory {
		case engine.Buy:
			c.buys++
		case engine.Sell:
			c.sells++
		}
		if r.Warmup {
			c.warmups++
		}
	}
}

func main() {
	core := engine.New(1000)
	highest, lastWatermark := int64(math.MinInt64), int64(math.MinInt64)
	var total counts
	for _, path := range datacheck.Paths() {
		var day counts
		lateBefore := core.Late
		datacheck.Scan(path, func(event csvparse.Event) {
			core.OnPrice(event.Symbol, event.SecType, event.Price, event.EventTimeMs)
			highest = max(highest, event.EventTimeMs)
			if highest >= lastWatermark+watermarkIntervalMs { // not highest-lastWatermark: overflows at the start
				day.add(core.OnWatermark(highest))
				lastWatermark = highest
			}
		})
		day.late = core.Late - lateBefore
		report(path, day)
		total.results, total.buys, total.sells, total.warmups, total.late =
			total.results+day.results, total.buys+day.buys, total.sells+day.sells, total.warmups+day.warmups, total.late+day.late
	}
	// Windows still open at the end of the last file; with the weekend files included there are none.
	var last counts
	last.add(core.OnWatermark(math.MaxInt64))
	report("final watermark", last)
	total.results, total.buys, total.sells, total.warmups =
		total.results+last.results, total.buys+last.buys, total.sells+last.sells, total.warmups+last.warmups
	report("total", total)
}

func report(name string, c counts) {
	fmt.Printf("%-50s results %9d | buy %6d (warmup %5d) | sell %6d | late %5d\n",
		name, c.results, c.buys, c.warmups, c.sells, c.late)
}
