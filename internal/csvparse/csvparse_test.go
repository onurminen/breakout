package csvparse

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

// testLine builds a 39-column data line for a valid price event, with overrides replacing columns.
func testLine(overrides map[int]string) []byte {
	fields := make([]string, 39)
	fields[ColID], fields[ColSecType], fields[ColDate] = "PX1.FR", "I", "08-11-2021"
	fields[ColLast], fields[ColTradingTime] = "7046.170000", "09:11:45.472"
	for column, value := range overrides {
		fields[column] = value
	}
	return []byte(strings.Join(fields, ",") + "\n")
}

func TestParseReasons(t *testing.T) {
	tests := []struct {
		name string
		line []byte
		want Reason
	}{
		{"valid", testLine(nil), OK},
		{"comment", []byte("# DEBS 2022 Grand Challenge\n"), Skip},
		{"header", []byte("ID,SecType,Date,Time,Ask\n"), Skip},
		{"empty", nil, Skip},
		{"housekeeping", testLine(map[int]string{ColLast: ""}), NoPrice},
		{"zero price", testLine(map[int]string{ColLast: "0.000000"}), BadPrice},
		{"negative price", testLine(map[int]string{ColLast: "-1.5"}), BadPrice},
		{"text price", testLine(map[int]string{ColLast: "abc"}), BadPrice},
		{"NaN price", testLine(map[int]string{ColLast: "NaN"}), BadPrice},
		{"infinite price", testLine(map[int]string{ColLast: "Inf"}), BadPrice},
		{"bad date", testLine(map[int]string{ColDate: "2021-11-08"}), BadTime},
		{"bad time", testLine(map[int]string{ColTradingTime: "9:11"}), BadTime},
		{"missing SecType", testLine(map[int]string{ColSecType: ""}), BadLine},
		{"unknown SecType", testLine(map[int]string{ColSecType: "X"}), BadLine},
		{"equity", testLine(map[int]string{ColSecType: "E"}), OK},
		{"short line", []byte("PX1.FR,I,08-11-2021\n"), BadLine},
	}
	parser := New()
	for _, test := range tests {
		if _, got := parser.Parse(test.line); got != test.want {
			t.Errorf("%s: got reason %d, want %d", test.name, got, test.want)
		}
	}
}

func TestParseEvent(t *testing.T) {
	want := Event{
		Symbol:      "PX1.FR",
		SecType:     'I',
		Price:       7046.17,
		EventTimeMs: 1636329600000 + (9*3600+11*60+45)*1000 + 472, // 08-11-2021 00:00 UTC + 09:11:45.472
	}
	parser := New()
	windowsLine := []byte(strings.TrimSuffix(string(testLine(nil)), "\n") + "\r\n")
	for _, input := range [][]byte{testLine(nil), windowsLine} {
		if got, _ := parser.Parse(input); got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	}
}

func TestParseTimeOfDay(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"00:00:00.000", 0, true},
		{"17:30:00.319", (17*3600+30*60)*1000 + 319, true},
		{"07:00:00", 7 * 3600 * 1000, true},
		{"07:00:00.5", 7*3600*1000 + 500, true},
		{"07:00:00.1234", 7*3600*1000 + 123, true},
		{"7:00:00", 0, false},
		{"", 0, false},
	}
	for _, test := range tests {
		got, ok := ParseTimeOfDay([]byte(test.input))
		if got != test.want || ok != test.ok {
			t.Errorf("ParseTimeOfDay(%q) = %d, %v; want %d, %v", test.input, got, ok, test.want, test.ok)
		}
	}
}

func TestParseDate(t *testing.T) {
	if got, ok := ParseDate([]byte("08-11-2021")); !ok || got != 1636329600000 {
		t.Errorf("ParseDate = %d, %v", got, ok)
	}
	if _, ok := ParseDate([]byte("2021-11-08")); ok {
		t.Error("ParseDate accepted wrong format")
	}
}

// TestRealFile parses a whole day file when BREAKOUT_CSV holds its absolute path:
//
//	BREAKOUT_CSV=$PWD/data/debs2022-gc-trading-day-08-11-21.csv go test -run RealFile -v ./internal/csvparse
func TestRealFile(t *testing.T) {
	path := os.Getenv("BREAKOUT_CSV")
	if path == "" {
		t.Skip("set BREAKOUT_CSV to a day file")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	parser := New()
	var counts [BadLine + 1]int
	reader := bufio.NewReaderSize(file, 1<<20)
	for {
		line, err := reader.ReadSlice('\n')
		if len(line) > 0 {
			_, reason := parser.Parse(line)
			counts[reason]++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("ok=%d skip=%d no_price=%d bad_price=%d bad_time=%d bad_line=%d",
		counts[OK], counts[Skip], counts[NoPrice], counts[BadPrice], counts[BadTime], counts[BadLine])
}

func BenchmarkParse(b *testing.B) {
	parser := New()
	input := testLine(nil)
	b.ReportAllocs()
	for b.Loop() {
		parser.Parse(input)
	}
}
