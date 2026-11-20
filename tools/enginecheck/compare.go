package main

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/onurminen/breakout/internal/engine"
)

// Rows as stored by the sink (deploy/postgres/init.sql). Comparing float64
// values directly, not as text, makes the check exact.
type emaRow struct {
	symbol        string
	secType       int16
	windowStartMs int64
	lastPrice     float64
	ema38, ema100 float64
}

type advisoryRow struct {
	symbol        string
	secType       int16
	windowStartMs int64
	side          int16
	ema38, ema100 float64
	warmup        bool
}

// compareWithDatabase reports whether both tables hold exactly the expected
// rows, and prints the first differences if not.
func compareWithDatabase(databaseURL string, results []engine.Result) bool {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		fmt.Println(err)
		return false
	}
	defer conn.Close(ctx)

	slices.SortFunc(results, func(a, b engine.Result) int {
		return cmp.Or(cmp.Compare(a.Symbol, b.Symbol), cmp.Compare(a.WindowStartMs, b.WindowStartMs))
	})
	var wantEma []emaRow
	var wantAdvisories []advisoryRow
	for _, r := range results {
		secType := int16(1) // SEC_TYPE_EQUITY
		if r.SecType == 'I' {
			secType = 2 // SEC_TYPE_INDEX
		}
		wantEma = append(wantEma, emaRow{r.Symbol, secType, r.WindowStartMs, r.LastPrice, r.EMA38, r.EMA100})
		if r.Advisory != engine.None {
			side := int16(1) // SIDE_BUY
			if r.Advisory == engine.Sell {
				side = 2 // SIDE_SELL
			}
			wantAdvisories = append(wantAdvisories, advisoryRow{r.Symbol, secType, r.WindowStartMs, side, r.EMA38, r.EMA100, r.Warmup})
		}
	}

	gotEma, err := query(ctx, conn, `SELECT symbol, sec_type, window_start_ms, last_price, ema38, ema100
		FROM ema_results ORDER BY symbol, window_start_ms`, func(row pgx.CollectableRow) (r emaRow, err error) {
		err = row.Scan(&r.symbol, &r.secType, &r.windowStartMs, &r.lastPrice, &r.ema38, &r.ema100)
		return r, err
	})
	if err != nil {
		fmt.Println(err)
		return false
	}
	gotAdvisories, err := query(ctx, conn, `SELECT symbol, sec_type, window_start_ms, side, ema38, ema100, warmup
		FROM advisories ORDER BY symbol, window_start_ms`, func(row pgx.CollectableRow) (r advisoryRow, err error) {
		err = row.Scan(&r.symbol, &r.secType, &r.windowStartMs, &r.side, &r.ema38, &r.ema100, &r.warmup)
		return r, err
	})
	if err != nil {
		fmt.Println(err)
		return false
	}
	emaOK := compareRows("ema_results", wantEma, gotEma)
	advisoriesOK := compareRows("advisories", wantAdvisories, gotAdvisories)
	return emaOK && advisoriesOK
}

func query[T any](ctx context.Context, conn *pgx.Conn, sql string, scan pgx.RowToFunc[T]) ([]T, error) {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scan)
}

// compareRows prints whether got equals want row by row, with the first few differences.
func compareRows[T comparable](table string, want, got []T) bool {
	differences := 0
	for i := range max(len(want), len(got)) {
		var w, g T
		if i < len(want) {
			w = want[i]
		}
		if i < len(got) {
			g = got[i]
		}
		if w != g {
			if differences < 5 {
				fmt.Printf("%s row %d: want %+v, got %+v\n", table, i, w, g)
			}
			differences++
		}
	}
	if differences > 0 {
		fmt.Printf("%s: %d of %d expected rows differ (database has %d)\n", table, differences, len(want), len(got))
		return false
	}
	fmt.Printf("%s: all %d rows match\n", table, len(want))
	return true
}
