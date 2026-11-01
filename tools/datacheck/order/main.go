// Command order checks that each symbol's events appear in the file in event-time
// order. If they do, ingestion can send lines in file order without sorting, and
// the engine can take the last price to arrive in a window as that window's price.
//
//	go run ./tools/datacheck/order data/debs2022-gc-trading-day-*.csv
package main

import (
	"fmt"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/tools/datacheck"
)

type orderCheck struct {
	latest    map[string]int64 // highest event time seen per symbol, across files
	events    int64
	behind    int64 // events older than an earlier event of the same symbol
	maxBehind int64
	worst     csvparse.Event
}

func (c *orderCheck) add(event csvparse.Event) {
	c.events++
	latest, seen := c.latest[event.Symbol]
	if seen && event.EventTimeMs < latest {
		c.behind++
		if latest-event.EventTimeMs > c.maxBehind {
			c.maxBehind, c.worst = latest-event.EventTimeMs, event
		}
		return
	}
	c.latest[event.Symbol] = event.EventTimeMs
}

func main() {
	latest := map[string]int64{}
	for _, path := range datacheck.Paths() {
		check := &orderCheck{latest: latest}
		datacheck.Scan(path, check.add)
		fmt.Printf("== %s\nprice events %d | behind an earlier event of their symbol %d\n", path, check.events, check.behind)
		if check.behind > 0 {
			fmt.Printf("furthest behind: %v, %s at %s\n",
				datacheck.Duration(check.maxBehind), check.worst.Symbol, datacheck.Clock(check.worst.EventTimeMs))
		}
		fmt.Println()
	}
}
