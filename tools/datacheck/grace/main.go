// Command grace counts the events that would arrive after their window has
// closed, for several grace periods. The engine's GRACE_MS is the smallest
// grace period that leaves only events no grace period can save.
//
//	go run ./tools/datacheck/grace data/debs2022-gc-trading-day-*.csv
package main

import (
	"fmt"
	"math"
	"sort"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/tools/datacheck"
)

var gracePeriodsMs = [...]int64{0, 1_000, 10_000, 60_000, 300_000}

type lateKey struct {
	eventTimeMs int64
	secType     byte
}

type graceCheck struct {
	// Highest event time seen so far. The real watermark is sent every second
	// and lags slightly behind, so the late counts here are an upper bound.
	watermark int64
	late      [len(gracePeriodsMs)]int64
	// Events still late at the largest grace period, grouped to show what they are.
	alwaysLate map[lateKey]int64
	maxBehind  int64 // largest distance of an event behind the watermark
	worst      csvparse.Event
}

func newGraceCheck(watermark int64) *graceCheck {
	return &graceCheck{watermark: watermark, alwaysLate: map[lateKey]int64{}}
}

func (c *graceCheck) add(event csvparse.Event) {
	if event.EventTimeMs >= c.watermark {
		c.watermark = event.EventTimeMs
		return
	}
	if behind := c.watermark - event.EventTimeMs; behind > c.maxBehind {
		c.maxBehind, c.worst = behind, event
	}
	// A window closes once the watermark reaches its end plus the grace period.
	windowEnd := (event.EventTimeMs/datacheck.WindowMs + 1) * datacheck.WindowMs
	for i, grace := range gracePeriodsMs {
		if windowEnd+grace <= c.watermark {
			c.late[i]++
		}
	}
	if windowEnd+gracePeriodsMs[len(gracePeriodsMs)-1] <= c.watermark {
		c.alwaysLate[lateKey{event.EventTimeMs, event.SecType}]++
	}
}

func main() {
	watermark := int64(math.MinInt64)
	for _, path := range datacheck.Paths() {
		check := newGraceCheck(watermark)
		datacheck.Scan(path, check.add)
		watermark = check.watermark
		report(path, check)
	}
}

func report(path string, check *graceCheck) {
	fmt.Printf("== %s\n", path)
	for i, grace := range gracePeriodsMs {
		fmt.Printf("late at grace %-5v %d\n", datacheck.Duration(grace), check.late[i])
	}
	if check.maxBehind > 0 {
		fmt.Printf("furthest behind: %v, %s at %s\n",
			datacheck.Duration(check.maxBehind), check.worst.Symbol, datacheck.Clock(check.worst.EventTimeMs))
	}
	keys := make([]lateKey, 0, len(check.alwaysLate))
	for key := range check.alwaysLate {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return check.alwaysLate[keys[i]] > check.alwaysLate[keys[j]] })
	for _, key := range keys[:min(len(keys), 5)] {
		fmt.Printf("late at every grace: %d events of SecType %c at %s\n",
			check.alwaysLate[key], key.secType, datacheck.Clock(key.eventTimeMs))
	}
	fmt.Println()
}
