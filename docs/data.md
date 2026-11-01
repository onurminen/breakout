# Data

One week (08–14 November 2021) of tick data from Paris `.FR`, Amsterdam `.NL` and Frankfurt/Xetra `.ETR`, one CSV file per day. A **price event** is a line with `Last` filled; all other lines are housekeeping events (quotes, status updates).

## Columns used

| # | Column | Example | Used as |
|---|---|---|---|
| 1 | `ID` | `PX1.FR` | Symbol, including the exchange suffix |
| 2 | `SecType` | `I` | `E` (equity) or `I` (index); passed on for filtering in the UI |
| 3 | `Date` | `08-11-2021` | Day of the event |
| 22 | `Last` | `7046.170000` | Price |
| 24 | `Trading time` | `09:11:45.472` | Time of day of the event |

Event time = `Date` + `Trading time`, as the assignment description prescribes. All other columns are ignored.

## Data checks

Each constant or assumption that depends on the data has a check in `tools/datacheck/`. The checks read the events the parser accepts, in file order, and take the day files in date order:

```bash
go run ./tools/datacheck/grace data/debs2022-gc-trading-day-*.csv
```

Saturday and Sunday have no valid price events and are left out below.

### Grace period (`grace`)

**Question:** how long must a window stay open after its end so that out-of-order events still count?

The check replays the file and keeps the highest event time seen so far as the watermark. An event is late if its window has already closed, that is, if the watermark has reached the window's end plus the grace period. It counts late events for several grace periods and reports the event furthest behind the watermark.

| Day | Late at 0 s | 1 s | 10 s | 1 min | 5 min | Furthest behind |
|---|---|---|---|---|---|---|
| Mon 08-11 | 79 | 0 | 0 | 0 | 0 | 64.5 s, at 17:30:00 |
| Tue 09-11 | 76 | 0 | 0 | 0 | 0 | 63.8 s, at 17:30:00 |
| Wed 10-11 | 5,560 | 5,506 | 5,506 | 5,506 | 5,506 | 17 h 56 min, at 01:00:00 |
| Thu 11-11 | 119 | 0 | 0 | 0 | 0 | 64.7 s, at 17:30:00 |
| Fri 12-11 | 24 | 0 | 0 | 0 | 0 | 64.8 s, at 17:30:00 |

**Result:** `GRACE_MS = 1000`, the smallest grace period with zero late events except Wednesday's 5,506. Those are all index events at 01:00:00.000 ([Anomalies](#anomalies)), late at every grace period.

### Order within each symbol (`order`)

**Question:** do each symbol's events appear in the file in event-time order?

The check remembers the highest event time seen per symbol and counts events older than it.

| Day | Price events | Older than an earlier event of their symbol |
|---|---|---|
| Mon 08-11 | 11,794,520 | 0 |
| Tue 09-11 | 11,900,121 | 0 |
| Wed 10-11 | 12,068,273 | 5,506 (the 01:00 events) |
| Thu 11-11 | 11,835,403 | 0 |
| Fri 12-11 | 11,793,353 | 0 |

**Result:** yes, apart from Wednesday's anomaly. Ingestion sends lines in file order without sorting. The disorder in the grace check comes from symbols interleaving.

### Empty windows (`windows`)

**Question:** how often does a symbol have no price at all in a window?

The check counts, per day and symbol, the windows that contain at least one price. It compares them with all 288 windows of the day, and with the windows between the symbol's first and last price that day.

| Day | Symbols | Windows with a price (per symbol) | Empty, whole day | Empty, between first and last price |
|---|---|---|---|---|
| Mon 08-11 | 4,840 | 373,126 (77.1) | 73.2% | 20.6% |
| Tue 09-11 | 4,851 | 371,307 (76.5) | 73.4% | 20.0% |
| Wed 10-11 | 4,848 | 372,175 (76.8) | 73.3% | 19.8% |
| Thu 11-11 | 4,835 | 368,727 (76.3) | 73.5% | 20.3% |
| Fri 12-11 | 4,843 | 370,310 (76.5) | 73.5% | 19.8% |

**Result:** empty windows are the normal case, even during trading (one in five). The engine skips them ([windowing.md](windowing.md#edge-cases)). The week produces about 1.86M results, one per window with a price.

## Anomalies

**Wednesday's 01:00 events.** Near the end of Wednesday's file, after events from 18:56, 5,506 index events appear with `Trading time` = `01:00:00.000`, with several different prices per symbol (e.g. `PX1.FR` at 7046.17, 7053.27, 7014.94). 01:00 looks like a placeholder. They arrive long after their window closed, so the engine drops them as late.

**Zero prices.** Some price events have `Last` = 0, including all of Saturday's. They are dropped (`BadPrice`).

## How ingestion treats each line

| Line | Treatment |
|---|---|
| Comment (`#…`) or header | skipped |
| `Last` empty (housekeeping) | dropped, counted |
| `Last` ≤ 0, NaN, infinite or not a number | dropped, counted |
| `Date` or `Trading time` unparsable | dropped, counted |
| Fewer than 24 columns, or `SecType` not `E`/`I` | dropped, counted |
| Anything else | becomes a price event |

The parser's real-file test prints these counts ([README](../README.md#getting-started)); on all seven files the last two rows never occur. Repeated prices are kept.
