package main

import (
	"testing"

	"github.com/onurminen/breakout/internal/csvparse"
)

func TestOrderCheck(t *testing.T) {
	check := &orderCheck{latest: map[string]int64{}}
	for _, event := range []csvparse.Event{
		{Symbol: "A", EventTimeMs: 1_000},
		{Symbol: "B", EventTimeMs: 500}, // older, but a different symbol
		{Symbol: "A", EventTimeMs: 1_000},
		{Symbol: "A", EventTimeMs: 400}, // behind A by 600 ms
		{Symbol: "A", EventTimeMs: 900}, // still behind A's latest
	} {
		check.add(event)
	}
	if check.events != 5 || check.behind != 2 || check.maxBehind != 600 {
		t.Errorf("events %d, behind %d, maxBehind %d", check.events, check.behind, check.maxBehind)
	}
}
