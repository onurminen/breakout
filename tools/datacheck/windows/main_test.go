package main

import (
	"testing"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/tools/datacheck"
)

func TestWindowCheck(t *testing.T) {
	check := &windowCheck{symbols: map[string]*symbolWindows{}}
	for _, event := range []csvparse.Event{
		{Symbol: "A", EventTimeMs: 0},
		{Symbol: "A", EventTimeMs: 1_000},                  // same window
		{Symbol: "A", EventTimeMs: 3 * datacheck.WindowMs}, // windows 1 and 2 empty
		{Symbol: "B", EventTimeMs: datacheck.WindowMs},
	} {
		check.add(event)
	}
	withPrice, active := check.totals()
	if withPrice != 3 || active != 5 {
		t.Errorf("withPrice %d, active %d, want 3, 5", withPrice, active)
	}
}
