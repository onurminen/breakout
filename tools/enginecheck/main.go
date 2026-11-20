// Command enginecheck runs day files through the engine core without Kafka and
// prints how many results, advisories and late events it produced. Watermarks
// are sent like ingestion sends them: whenever the highest event time seen has
// advanced by a second, and a final one at the end.
//
//	go run ./tools/enginecheck data/debs2022-gc-trading-day-*.csv
//
// -symbols replays only some symbols, like ingestion's SYMBOLS. -db then
// compares every result with the Postgres tables the pipeline filled, for the
// end-to-end check (docs/evaluation.md#correctness):
//
//	go run ./tools/enginecheck -symbols PX1.FR,RDSA.NL -db postgres://breakout:breakout@localhost:5432/breakout data/debs2022-gc-trading-day-08-11-21.csv
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strings"

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
	symbolList := flag.String("symbols", "", "comma-separated symbols to replay; empty: all")
	databaseURL := flag.String("db", "", "Postgres URL; compare the results with its tables")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintf(os.Stderr, "usage: %s [-symbols A,B] [-db URL] DAY.csv [DAY.csv ...]\n", os.Args[0])
		os.Exit(2)
	}
	var symbols map[string]bool // nil: all symbols
	if *symbolList != "" {
		symbols = map[string]bool{}
		for _, symbol := range strings.Split(*symbolList, ",") {
			symbols[symbol] = true
		}
	}

	core := engine.New(1000)
	highest, lastWatermark := int64(math.MinInt64), int64(math.MinInt64)
	var total counts
	var all []engine.Result // kept only for -db
	closeWindows := func(watermarkMs int64) []engine.Result {
		results := core.OnWatermark(watermarkMs)
		if *databaseURL != "" {
			all = append(all, results...)
		}
		return results
	}
	for _, path := range flag.Args() {
		var day counts
		lateBefore := core.Late
		datacheck.Scan(path, func(event csvparse.Event) {
			if symbols != nil && !symbols[event.Symbol] {
				return
			}
			core.OnPrice(event.Symbol, event.SecType, event.Price, event.EventTimeMs)
			highest = max(highest, event.EventTimeMs)
			if highest >= lastWatermark+watermarkIntervalMs { // not highest-lastWatermark: overflows at the start
				day.add(closeWindows(highest))
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
	last.add(closeWindows(math.MaxInt64))
	report("final watermark", last)
	total.results, total.buys, total.sells, total.warmups =
		total.results+last.results, total.buys+last.buys, total.sells+last.sells, total.warmups+last.warmups
	report("total", total)

	if *databaseURL != "" && !compareWithDatabase(*databaseURL, all) {
		os.Exit(1)
	}
}

func report(name string, c counts) {
	fmt.Printf("%-50s results %9d | buy %6d (warmup %5d) | sell %6d | late %5d\n",
		name, c.results, c.buys, c.warmups, c.sells, c.late)
}
