package store

import (
	"context"
	"os"
	"testing"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
)

// testSymbol cannot clash with a real symbol, so the test can run against the Compose database.
const testSymbol = "STORETEST.X"

// connect skips the test unless DATABASE_URL points to a database created by init.sql:
//
//	DATABASE_URL=postgres://breakout:breakout@localhost:5432/breakout go test ./internal/store
func connect(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	s, err := Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	removeTestRows := func() {
		for _, table := range []string{"ema_results", "advisories"} {
			if _, err := s.pool.Exec(context.Background(), "DELETE FROM "+table+" WHERE symbol = $1", testSymbol); err != nil {
				t.Fatal(err)
			}
		}
	}
	removeTestRows()
	t.Cleanup(func() { removeTestRows(); s.Close() })
	return s
}

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var count int
	err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE symbol = $1", testSymbol).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestInsertTwiceKeepsOneCopy(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	results := []*resultsv1.EmaResult{
		{Symbol: testSymbol, SecType: ticksv1.SecType_SEC_TYPE_INDEX, WindowStartMs: 0, LastPrice: 100, Ema38: 5.1, Ema100: 2.0},
		{Symbol: testSymbol, SecType: ticksv1.SecType_SEC_TYPE_INDEX, WindowStartMs: 300_000, LastPrice: 90, Ema38: 5.3, Ema100: 2.1},
	}
	advisories := []*resultsv1.Advisory{
		{Symbol: testSymbol, SecType: ticksv1.SecType_SEC_TYPE_INDEX, WindowStartMs: 0, Side: resultsv1.Side_SIDE_BUY, Ema38: 5.1, Ema100: 2.0, Warmup: true},
	}

	// The second attempt repeats the batch, as after a crash before the offset commit.
	for attempt, want := range []struct{ results, advisories int64 }{{2, 1}, {0, 0}} {
		newResults, newAdvisories, err := s.Insert(ctx, results, advisories)
		if err != nil || newResults != want.results || newAdvisories != want.advisories {
			t.Errorf("attempt %d: Insert = %d, %d, %v; want %d and %d new rows",
				attempt, newResults, newAdvisories, err, want.results, want.advisories)
		}
	}
	if n := countRows(t, s, "ema_results"); n != 2 {
		t.Errorf("ema_results has %d rows, want 2", n)
	}
	if n := countRows(t, s, "advisories"); n != 1 {
		t.Errorf("advisories has %d rows, want 1", n)
	}

	var side int16
	var warmup bool
	err := s.pool.QueryRow(ctx, "SELECT side, warmup FROM advisories WHERE symbol = $1", testSymbol).Scan(&side, &warmup)
	if err != nil || side != 1 || !warmup {
		t.Errorf("advisory side %d warmup %v, %v; want 1 (buy) and true", side, warmup, err)
	}
}
