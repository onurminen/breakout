package main

import (
	"math"
	"time"

	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/internal/stream"
)

const watermarkIntervalMs = 1000

// sendFunc delivers one message to a partition of ticks; key is empty for watermarks.
type sendFunc func(partition int32, key string, message *ticksv1.TickMessage)

// replay turns price events into ticks messages: each event to its symbol's
// partition, and a watermark to every partition whenever the highest event
// time sent has advanced by watermarkIntervalMs (docs/windowing.md#watermarks).
type replay struct {
	send  sendFunc
	now   func() time.Time
	sleep func(time.Duration)
	speed float64 // event-time seconds per wall-clock second; 0 = as fast as possible

	highest, lastWatermark int64
	pacing                 bool
	pacingStartMs          int64
	pacingStartWall        time.Time
}

func newReplay(send sendFunc, speed float64) *replay {
	return &replay{
		send:          send,
		now:           time.Now,
		sleep:         time.Sleep,
		speed:         speed,
		highest:       math.MinInt64,
		lastWatermark: math.MinInt64,
	}
}

func (r *replay) onEvent(event csvparse.Event) {
	r.highest = max(r.highest, event.EventTimeMs)
	r.pace()

	nowNs := r.now().UnixNano()
	secType := ticksv1.SecType_SEC_TYPE_EQUITY
	if event.SecType == 'I' {
		secType = ticksv1.SecType_SEC_TYPE_INDEX
	}
	r.send(stream.Partition(event.Symbol), event.Symbol, &ticksv1.TickMessage{
		Payload: &ticksv1.TickMessage_Price{Price: &ticksv1.PriceEvent{
			Symbol:       event.Symbol,
			SecType:      secType,
			Price:        event.Price,
			EventTimeMs:  event.EventTimeMs,
			IngestTimeNs: nowNs,
		}},
	})

	if r.highest >= r.lastWatermark+watermarkIntervalMs { // not highest-lastWatermark: overflows at the start
		r.sendWatermark(r.highest, nowNs)
		r.lastWatermark = r.highest
	}
}

// finish sends the final watermark, which closes every window still open.
func (r *replay) finish() {
	r.sendWatermark(math.MaxInt64, r.now().UnixNano())
}

func (r *replay) sendWatermark(eventTimeMs, nowNs int64) {
	message := &ticksv1.TickMessage{Payload: &ticksv1.TickMessage_Watermark{Watermark: &ticksv1.Watermark{
		EventTimeMs:  eventTimeMs,
		IngestTimeNs: nowNs,
	}}}
	for partition := range int32(stream.TicksPartitions) {
		r.send(partition, "", message)
	}
}

// pace sleeps until the wall clock catches up with the highest event time.
// Pacing on the highest time, not the event's own, keeps out-of-order events
// such as Wednesday's 01:00 ones from asking for negative sleeps. Sleeping only
// when more than a millisecond ahead avoids a sleep per event at high rates.
func (r *replay) pace() {
	if r.speed == 0 {
		return
	}
	now := r.now()
	if !r.pacing {
		r.pacing, r.pacingStartMs, r.pacingStartWall = true, r.highest, now
		return
	}
	elapsed := time.Duration(float64(r.highest-r.pacingStartMs) / r.speed * float64(time.Millisecond))
	if ahead := r.pacingStartWall.Add(elapsed).Sub(now); ahead > time.Millisecond {
		r.sleep(ahead)
	}
}
