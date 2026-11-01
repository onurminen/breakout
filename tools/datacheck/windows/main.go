// Command windows counts how many of each symbol's 5-minute windows contain no
// price. Empty windows produce no result, so this decides how often the engine
// skips a window and how many results there are to store.
//
//	go run ./tools/datacheck/windows data/debs2022-gc-trading-day-*.csv
package main

import (
	"fmt"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/tools/datacheck"
)

const windowsPerDay = 24 * 60 * 60 * 1000 / datacheck.WindowMs

type symbolWindows struct {
	first, last, withPrice int64 // first and last window with a price, and how many had one
}

// windowCheck covers one day file.
type windowCheck struct {
	symbols map[string]*symbolWindows
}

func (c *windowCheck) add(event csvparse.Event) {
	window := event.EventTimeMs / datacheck.WindowMs
	symbol := c.symbols[event.Symbol]
	if symbol == nil {
		c.symbols[event.Symbol] = &symbolWindows{first: window, last: window, withPrice: 1}
		return
	}
	// Each symbol's events are in order (see the order check), so a new window shows up as a larger index.
	if window > symbol.last {
		symbol.last = window
		symbol.withPrice++
	}
}

// totals returns the windows with a price, and the windows between each symbol's first and last price.
func (c *windowCheck) totals() (withPrice, active int64) {
	for _, symbol := range c.symbols {
		withPrice += symbol.withPrice
		active += symbol.last - symbol.first + 1
	}
	return withPrice, active
}

func main() {
	for _, path := range datacheck.Paths() {
		check := &windowCheck{symbols: map[string]*symbolWindows{}}
		datacheck.Scan(path, check.add)
		fmt.Printf("== %s\n", path)
		symbols := int64(len(check.symbols))
		if symbols == 0 {
			fmt.Print("no price events\n\n")
			continue
		}
		withPrice, active := check.totals()
		all := symbols * windowsPerDay
		fmt.Printf("symbols %d | windows with a price %d (%.1f per symbol)\n",
			symbols, withPrice, float64(withPrice)/float64(symbols))
		fmt.Printf("empty: %.1f%% of all %d windows, %.1f%% of the %d between a symbol's first and last price\n\n",
			100*float64(all-withPrice)/float64(all), all, 100*float64(active-withPrice)/float64(active), active)
	}
}
