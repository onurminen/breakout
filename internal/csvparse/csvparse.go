// Package csvparse turns lines of the DEBS 2022 trading CSVs into price events.
package csvparse

import (
	"bytes"
	"math"
	"strconv"
	"time"
)

// 0-based CSV columns used for events.
const (
	ColID          = 0
	ColSecType     = 1
	ColDate        = 2
	ColLast        = 21
	ColTradingTime = 23
)

// Event is one valid price event.
type Event struct {
	Symbol      string  // ID including the exchange, e.g. "PX1.FR"
	SecType     byte    // 'E' (equity) or 'I' (index)
	Price       float64 // Last
	EventTimeMs int64   // Date + Trading time as epoch ms, treated as UTC
}

// Reason says why Parse did or did not return an event.
type Reason int

const (
	OK       Reason = iota
	Skip            // comment, header or empty line; not an event
	NoPrice         // Last empty: housekeeping event
	BadPrice        // Last <= 0, infinite or unparsable
	BadTime         // Date or Trading time unparsable
	BadLine         // too few columns, or SecType not E or I
)

// Parser parses lines one at a time. It caches dates and symbol strings,
// so it is not safe for concurrent use: give each goroutine its own Parser.
type Parser struct {
	fields  [ColTradingTime + 1][]byte
	dates   map[string]int64  // "08-11-2021" → epoch ms at midnight
	symbols map[string]string // one shared string per symbol instead of one per event
}

func New() *Parser {
	return &Parser{dates: map[string]int64{}, symbols: map[string]string{}}
}

// Parse parses one line, with or without its trailing newline.
// The Event is only meaningful when the Reason is OK.
func (p *Parser) Parse(line []byte) (Event, Reason) {
	if len(line) == 0 || line[0] == '#' || bytes.HasPrefix(line, []byte("ID,")) {
		return Event{}, Skip
	}
	fields := &p.fields
	if Split(line, fields[:]) < len(fields) {
		return Event{}, BadLine
	}
	if len(fields[ColLast]) == 0 {
		return Event{}, NoPrice
	}
	price, err := strconv.ParseFloat(string(fields[ColLast]), 64)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return Event{}, BadPrice
	}
	if secType := fields[ColSecType]; len(secType) != 1 || (secType[0] != 'E' && secType[0] != 'I') {
		return Event{}, BadLine
	}

	dayStartMs, dateOK := p.dates[string(fields[ColDate])]
	if !dateOK {
		if dayStartMs, dateOK = ParseDate(fields[ColDate]); dateOK {
			p.dates[string(fields[ColDate])] = dayStartMs
		}
	}
	timeOfDayMs, timeOK := ParseTimeOfDay(fields[ColTradingTime])
	if !dateOK || !timeOK {
		return Event{}, BadTime
	}

	symbol, known := p.symbols[string(fields[ColID])]
	if !known {
		symbol = string(fields[ColID])
		p.symbols[symbol] = symbol
	}
	return Event{
		Symbol:      symbol,
		SecType:     fields[ColSecType][0],
		Price:       price,
		EventTimeMs: dayStartMs + timeOfDayMs,
	}, OK
}

// Split fills fields with the first len(fields) comma-separated fields of line,
// without its trailing newline, and returns how many it filled. The fields
// share line's memory. Data lines contain no quoted fields.
func Split(line []byte, fields [][]byte) int {
	line = bytes.TrimRight(line, "\r\n")
	for index := range fields {
		comma := bytes.IndexByte(line, ',')
		if comma < 0 {
			fields[index] = line
			return index + 1
		}
		fields[index], line = line[:comma], line[comma+1:]
	}
	return len(fields)
}

// ParseDate parses DD-MM-YYYY to epoch ms at midnight, treating it as UTC.
func ParseDate(date []byte) (int64, bool) {
	if len(date) != 10 || date[2] != '-' || date[5] != '-' {
		return 0, false
	}
	day, dayOK := parseDigits(date[0:2])
	month, monthOK := parseDigits(date[3:5])
	year, yearOK := parseDigits(date[6:10])
	if !dayOK || !monthOK || !yearOK {
		return 0, false
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC).UnixMilli(), true
}

// ParseTimeOfDay parses HH:MM:SS[.fff] to ms since midnight; digits past ms are ignored.
func ParseTimeOfDay(clock []byte) (int64, bool) {
	if len(clock) < 8 || clock[2] != ':' || clock[5] != ':' {
		return 0, false
	}
	hours, hoursOK := parseDigits(clock[0:2])
	minutes, minutesOK := parseDigits(clock[3:5])
	seconds, secondsOK := parseDigits(clock[6:8])
	if !hoursOK || !minutesOK || !secondsOK {
		return 0, false
	}
	millis := 0
	if len(clock) > 8 {
		if clock[8] != '.' {
			return 0, false
		}
		fraction := clock[9:]
		for i := range 3 {
			millis *= 10
			if i < len(fraction) {
				if fraction[i] < '0' || fraction[i] > '9' {
					return 0, false
				}
				millis += int(fraction[i] - '0')
			}
		}
	}
	return int64(((hours*60+minutes)*60+seconds)*1000 + millis), true
}

// parseDigits parses a non-empty run of ASCII digits.
func parseDigits(digits []byte) (int, bool) {
	value := 0
	for _, char := range digits {
		if char < '0' || char > '9' {
			return 0, false
		}
		value = value*10 + int(char-'0')
	}
	return value, len(digits) > 0
}
