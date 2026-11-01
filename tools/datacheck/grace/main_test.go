package main

import (
	"math"
	"testing"

	"github.com/onurminen/breakout/internal/csvparse"
)

func TestGraceCheck(t *testing.T) {
	check := newGraceCheck(math.MinInt64)
	for _, eventTimeMs := range []int64{
		299_000, // window 0
		300_500, // window 1: watermark 300.5 s closes window 0 at grace 0 only
		299_500, // late at grace 0
		900_000, // watermark 15 min closes window 0 at every grace up to 5 min
		200_000, // late at every grace
	} {
		check.add(csvparse.Event{Symbol: "A", SecType: 'I', EventTimeMs: eventTimeMs})
	}
	if want := [...]int64{2, 1, 1, 1, 1}; check.late != want {
		t.Errorf("late = %v, want %v", check.late, want)
	}
	if check.maxBehind != 700_000 || check.alwaysLate[lateKey{200_000, 'I'}] != 1 {
		t.Errorf("maxBehind = %d, alwaysLate = %v", check.maxBehind, check.alwaysLate)
	}
}
