package main

import (
	"strings"
	"testing"

	"github.com/onurminen/breakout/internal/csvparse"
)

func TestTradingDateCheck(t *testing.T) {
	head := strings.Repeat("x,", 26) // columns 0–25
	check := &tradingDateCheck{}
	for _, line := range []string{
		head + "08-11-2021,y\n",
		head + ",y\n",    // empty
		head + "\n",      // last column, empty
		"A,E,08-11-2021", // too few columns
	} {
		check.add(csvparse.Event{}, []byte(line))
	}
	if check.events != 4 || check.empty != 3 {
		t.Errorf("events %d, empty %d", check.events, check.empty)
	}
}
