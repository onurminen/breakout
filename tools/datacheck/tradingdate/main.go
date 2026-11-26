// Command tradingdate counts the valid price events whose Trading date is empty.
//
//	go run ./tools/datacheck/tradingdate data/debs2022-gc-trading-day-*.csv
package main

import (
	"fmt"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/tools/datacheck"
)

const colTradingDate = 26 // 0-based CSV column "Trading date"

type tradingDateCheck struct {
	fields [colTradingDate + 1][]byte
	events int64
	empty  int64
}

func (c *tradingDateCheck) add(_ csvparse.Event, line []byte) {
	c.events++
	if csvparse.Split(line, c.fields[:]) <= colTradingDate || len(c.fields[colTradingDate]) == 0 {
		c.empty++
	}
}

func main() {
	for _, path := range datacheck.Paths() {
		check := &tradingDateCheck{}
		datacheck.ScanLines(path, check.add)
		share := 0.0
		if check.events > 0 {
			share = 100 * float64(check.empty) / float64(check.events)
		}
		fmt.Printf("== %s\nprice events %d | empty Trading date %d (%.1f%%)\n\n", path, check.events, check.empty, share)
	}
}
