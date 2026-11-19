package main

import (
	"context"
	"errors"
	"log"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/onurminen/breakout/internal/stream"
)

// consumer runs one goroutine per assigned partition of ticks, following
// franz-go's goroutine-per-partition example. With BlockRebalanceOnPoll the
// assign and revoke callbacks never run while poll uses the workers map, so the
// map needs no mutex.
type consumer struct {
	client  *kgo.Client
	graceMs int64
	workers map[int32]*partitionWorker
}

type partitionWorker struct {
	records chan []*kgo.Record
	quit    chan struct{}
	done    chan struct{}
}

func (c *consumer) assigned(_ context.Context, client *kgo.Client, assigned map[string][]int32) {
	for _, partition := range assigned[stream.TicksTopic] {
		pw := &partitionWorker{
			records: make(chan []*kgo.Record, 4),
			quit:    make(chan struct{}),
			done:    make(chan struct{}),
		}
		c.workers[partition] = pw
		go pw.run(newWorker(c.graceMs, func(r *kgo.Record) { produce(client, r) }))
	}
	log.Printf("assigned partitions %v", assigned[stream.TicksTopic])
}

// revoked stops the partitions' workers and waits for them. Their state is
// lost; whoever gets the partition next starts again from offset 0.
func (c *consumer) revoked(_ context.Context, _ *kgo.Client, revoked map[string][]int32) {
	for _, partition := range revoked[stream.TicksTopic] {
		if pw := c.workers[partition]; pw != nil {
			close(pw.quit)
			<-pw.done
			delete(c.workers, partition)
		}
	}
	log.Printf("revoked partitions %v", revoked[stream.TicksTopic])
}

func (c *consumer) poll(ctx context.Context) {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			log.Fatalf("fetch %s/%d: %v", topic, partition, err)
		})
		fetches.EachPartition(func(p kgo.FetchTopicPartition) {
			if len(p.Records) > 0 {
				c.workers[p.Partition].records <- p.Records
			}
		})
		c.client.AllowRebalance()
	}
}

func (pw *partitionWorker) run(w *worker) {
	defer close(pw.done)
	for {
		select {
		case <-pw.quit:
			return
		case records := <-pw.records:
			if err := w.handle(records); err != nil {
				log.Fatal(err)
			}
		}
	}
}

// produce sends asynchronously; a failed send stops the engine, since a missing
// result would silently leave a gap in the history. Records still buffered at
// shutdown are dropped: no offsets are committed, so the next run recomputes them.
func produce(client *kgo.Client, record *kgo.Record) {
	client.Produce(context.Background(), record, func(record *kgo.Record, err error) {
		if err != nil && !errors.Is(err, kgo.ErrClientClosed) {
			log.Fatalf("produce to %s: %v", record.Topic, err)
		}
	})
}
