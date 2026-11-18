// Package stream holds what the services share about Kafka: topic names, the
// partitioning of ticks and client construction (docs/architecture.md#kafka-topics).
package stream

import (
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	TicksTopic = "ticks"
	// TicksPartitions must match the topic created in deploy/docker-compose.yml and
	// must not change while data flows, or symbols would move to other partitions.
	TicksPartitions = 12
)

// Partition returns the ticks partition of a symbol: FNV-1a % TicksPartitions.
// The hash is written out because hash/fnv allocates on every call.
func Partition(symbol string) int32 {
	hash := uint32(2166136261)
	for i := range len(symbol) {
		hash ^= uint32(symbol[i])
		hash *= 16777619
	}
	return int32(hash % TicksPartitions)
}

// NewClient connects to the comma-separated seed brokers.
func NewClient(brokers string, options ...kgo.Opt) (*kgo.Client, error) {
	return kgo.NewClient(append([]kgo.Opt{kgo.SeedBrokers(strings.Split(brokers, ",")...)}, options...)...)
}
