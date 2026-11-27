// Command pricedrops counts, per file, the lines the parser turns into price
// events and the lines it drops, by reason.
//
//	go run ./tools/datacheck/pricedrops data/debs2022-gc-trading-day-*.csv
package main

import (
	"fmt"

	"github.com/onurminen/breakout/internal/csvparse"
	"github.com/onurminen/breakout/tools/datacheck"
)

type dropCheck struct {
	parser *csvparse.Parser
	counts [csvparse.BadLine + 1]int64
}

func (c *dropCheck) add(line []byte) {
	_, reason := c.parser.Parse(line)
	c.counts[reason]++
}

func main() {
	for _, path := range datacheck.Paths() {
		check := &dropCheck{parser: csvparse.New()}
		datacheck.Lines(path, check.add)
		counts := check.counts
		dataLines := counts[csvparse.OK] + counts[csvparse.NoPrice] + counts[csvparse.BadPrice] +
			counts[csvparse.BadTime] + counts[csvparse.BadLine]
		dropped := dataLines - counts[csvparse.OK]
		share := 0.0
		if dataLines > 0 {
			share = 100 * float64(dropped) / float64(dataLines)
		}
		fmt.Printf("== %s\ndata lines %d | price events %d | dropped %d (%.1f%%)\n", path, dataLines, counts[csvparse.OK], dropped, share)
		fmt.Printf("dropped: empty Last %d | Last <= 0 or not a number %d | bad time %d | bad line %d | skipped (comments, header) %d\n\n",
			counts[csvparse.NoPrice], counts[csvparse.BadPrice], counts[csvparse.BadTime], counts[csvparse.BadLine], counts[csvparse.Skip])
	}
}
