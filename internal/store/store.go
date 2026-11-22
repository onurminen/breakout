// Package store reads and writes the result history in Postgres
// (deploy/postgres/init.sql, docs/architecture.md#storage).
package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	resultsv1 "github.com/onurminen/breakout/gen/go/breakout/results/v1"
	ticksv1 "github.com/onurminen/breakout/gen/go/breakout/ticks/v1"
)

// Store is a pool of connections to the results database. It is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
}

// Connect creates the pool; connections are opened when first needed.
func Connect(databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

type Symbol struct {
	Symbol  string
	SecType ticksv1.SecType
}

// Symbols returns the symbols for which the sink has stored at least one window.
func (s *Store) Symbols(ctx context.Context) ([]Symbol, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT symbol, sec_type FROM ema_results ORDER BY symbol, sec_type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	symbols := make([]Symbol, 0)
	for rows.Next() {
		var symbol Symbol
		var secType int16
		if err := rows.Scan(&symbol.Symbol, &secType); err != nil {
			return nil, err
		}
		symbol.SecType = ticksv1.SecType(secType)
		symbols = append(symbols, symbol)
	}
	return symbols, rows.Err()
}

// History returns both result types for one symbol, ordered by window start.
// The time range is [fromMs, toMs). A read-only transaction gives both queries
// the same snapshot while the sink is writing.
func (s *Store) History(ctx context.Context, symbol string, fromMs, toMs int64) ([]*resultsv1.EmaResult, []*resultsv1.Advisory, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback(ctx)

	results := make([]*resultsv1.EmaResult, 0)
	rows, err := tx.Query(ctx, `SELECT symbol, sec_type, window_start_ms, last_price, ema38, ema100
		FROM ema_results WHERE symbol = $1 AND window_start_ms >= $2 AND window_start_ms < $3
		ORDER BY window_start_ms`, symbol, fromMs, toMs)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		r := &resultsv1.EmaResult{}
		var secType int16
		if err := rows.Scan(&r.Symbol, &secType, &r.WindowStartMs, &r.LastPrice, &r.Ema38, &r.Ema100); err != nil {
			rows.Close()
			return nil, nil, err
		}
		r.SecType = ticksv1.SecType(secType)
		results = append(results, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}

	advisories := make([]*resultsv1.Advisory, 0)
	rows, err = tx.Query(ctx, `SELECT symbol, sec_type, window_start_ms, side, ema38, ema100, warmup
		FROM advisories WHERE symbol = $1 AND window_start_ms >= $2 AND window_start_ms < $3
		ORDER BY window_start_ms`, symbol, fromMs, toMs)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		a := &resultsv1.Advisory{}
		var secType, side int16
		if err := rows.Scan(&a.Symbol, &secType, &a.WindowStartMs, &side, &a.Ema38, &a.Ema100, &a.Warmup); err != nil {
			rows.Close()
			return nil, nil, err
		}
		a.SecType, a.Side = ticksv1.SecType(secType), resultsv1.Side(side)
		advisories = append(advisories, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	return results, advisories, nil
}

// The arrays keep each statement the same whatever the batch size. Rows that
// are already stored are skipped, so writing a batch again changes nothing.
const (
	insertEmaResultsSQL = `
		INSERT INTO ema_results (symbol, sec_type, window_start_ms, last_price, ema38, ema100)
		SELECT * FROM unnest($1::text[], $2::smallint[], $3::bigint[], $4::float8[], $5::float8[], $6::float8[])
		ON CONFLICT DO NOTHING`
	insertAdvisoriesSQL = `
		INSERT INTO advisories (symbol, sec_type, window_start_ms, side, ema38, ema100, warmup)
		SELECT * FROM unnest($1::text[], $2::smallint[], $3::bigint[], $4::smallint[], $5::float8[], $6::float8[], $7::bool[])
		ON CONFLICT DO NOTHING`
)

// Insert writes a batch to both tables in one transaction, with one statement
// per table, and returns how many rows of each were new. Either the whole batch
// is stored or none of it.
func (s *Store) Insert(ctx context.Context, results []*resultsv1.EmaResult, advisories []*resultsv1.Advisory) (newResults, newAdvisories int64, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if newResults, err = insertEmaResults(ctx, tx, results); err != nil {
			return err
		}
		newAdvisories, err = insertAdvisories(ctx, tx, advisories)
		return err
	})
	return newResults, newAdvisories, err
}

func insertEmaResults(ctx context.Context, tx pgx.Tx, results []*resultsv1.EmaResult) (int64, error) {
	if len(results) == 0 {
		return 0, nil
	}
	n := len(results)
	symbols, secTypes, windowStartsMs := make([]string, n), make([]int16, n), make([]int64, n)
	lastPrices, ema38s, ema100s := make([]float64, n), make([]float64, n), make([]float64, n)
	for i, r := range results {
		symbols[i], secTypes[i], windowStartsMs[i] = r.Symbol, int16(r.SecType), r.WindowStartMs
		lastPrices[i], ema38s[i], ema100s[i] = r.LastPrice, r.Ema38, r.Ema100
	}
	tag, err := tx.Exec(ctx, insertEmaResultsSQL, symbols, secTypes, windowStartsMs, lastPrices, ema38s, ema100s)
	return tag.RowsAffected(), err
}

func insertAdvisories(ctx context.Context, tx pgx.Tx, advisories []*resultsv1.Advisory) (int64, error) {
	if len(advisories) == 0 {
		return 0, nil
	}
	n := len(advisories)
	symbols, secTypes, windowStartsMs, sides := make([]string, n), make([]int16, n), make([]int64, n), make([]int16, n)
	ema38s, ema100s, warmups := make([]float64, n), make([]float64, n), make([]bool, n)
	for i, a := range advisories {
		symbols[i], secTypes[i], windowStartsMs[i], sides[i] = a.Symbol, int16(a.SecType), a.WindowStartMs, int16(a.Side)
		ema38s[i], ema100s[i], warmups[i] = a.Ema38, a.Ema100, a.Warmup
	}
	tag, err := tx.Exec(ctx, insertAdvisoriesSQL, symbols, secTypes, windowStartsMs, sides, ema38s, ema100s, warmups)
	return tag.RowsAffected(), err
}
