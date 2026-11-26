// Package datacheck holds what the data checks in its subfolders share. Each
// check answers one question about the DEBS 2022 data that a constant or an
// assumption in the engine depends on (see docs/data.md#data-checks).
//
// Every check reads the same events the system will see: lines the parser
// accepts, in file order. Pass the day files in date order; state that spans
// days carries across files.
package datacheck

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/onurminen/breakout/internal/csvparse"
)

// WindowMs is the length of one tumbling window.
const WindowMs = 5 * 60 * 1000

// Paths returns the CSV paths given on the command line, or exits with a usage message.
func Paths() []string {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s DAY.csv [DAY.csv ...]\n", os.Args[0])
		os.Exit(2)
	}
	return os.Args[1:]
}

// Scan calls handle for every valid price event in the file, in file order.
func Scan(path string, handle func(csvparse.Event)) {
	ScanLines(path, func(event csvparse.Event, _ []byte) { handle(event) })
}

// ScanLines is Scan that also passes the raw line, which is only valid during the call.
func ScanLines(path string, handle func(csvparse.Event, []byte)) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer file.Close()

	parser := csvparse.New()
	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		line, err := reader.ReadSlice('\n')
		if event, reason := parser.Parse(line); reason == csvparse.OK {
			handle(event, line)
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
	}
}

// Duration turns milliseconds into a printable duration.
func Duration(millis int64) time.Duration { return time.Duration(millis) * time.Millisecond }

// Clock formats an event time as date and time of day.
func Clock(eventTimeMs int64) string {
	return time.UnixMilli(eventTimeMs).UTC().Format("02-01-2006 15:04:05.000")
}
