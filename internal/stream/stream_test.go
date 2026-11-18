package stream

import (
	"hash/fnv"
	"testing"
)

func TestPartition(t *testing.T) {
	seen := map[int32]bool{}
	for _, symbol := range []string{"PX1.FR", "RDSA.NL", "ALVG.DE", "IEBBB.FR", "A", ""} {
		reference := fnv.New32a()
		reference.Write([]byte(symbol))
		want := int32(reference.Sum32() % TicksPartitions)

		if got := Partition(symbol); got != want {
			t.Errorf("Partition(%q) = %d, want %d", symbol, got, want)
		}
		seen[want] = true
	}
	if len(seen) < 2 {
		t.Errorf("all symbols in one partition")
	}
}
