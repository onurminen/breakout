package main

import (
	"strings"
	"testing"

	"github.com/onurminen/breakout/internal/csvparse"
)

func TestDropCheck(t *testing.T) {
	line := func(last string) string {
		fields := make([]string, 24)
		fields[0], fields[1], fields[2], fields[21], fields[23] = "PX1.FR", "I", "08-11-2021", last, "09:11:45.472"
		return strings.Join(fields, ",") + "\n"
	}
	check := &dropCheck{parser: csvparse.New()}
	for _, l := range []string{"# comment\n", line("7046.17"), line(""), line(""), line("0")} {
		check.add([]byte(l))
	}
	want := map[csvparse.Reason]int64{csvparse.OK: 1, csvparse.Skip: 1, csvparse.NoPrice: 2, csvparse.BadPrice: 1}
	for reason, count := range want {
		if check.counts[reason] != count {
			t.Errorf("reason %d: got %d, want %d", reason, check.counts[reason], count)
		}
	}
}
