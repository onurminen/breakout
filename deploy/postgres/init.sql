-- Runs once, when the Postgres container starts with an empty data directory.
-- Columns mirror EmaResult and Advisory in proto/breakout/results/v1/results.proto.

CREATE TABLE ema_results (
    symbol          text             NOT NULL,
    sec_type        smallint         NOT NULL, -- 1 = equity, 2 = index
    window_start_ms bigint           NOT NULL,
    last_price      double precision NOT NULL,
    ema38           double precision NOT NULL,
    ema100          double precision NOT NULL,
    PRIMARY KEY (symbol, window_start_ms)
);

CREATE TABLE advisories (
    symbol          text             NOT NULL,
    sec_type        smallint         NOT NULL, -- 1 = equity, 2 = index
    window_start_ms bigint           NOT NULL,
    side            smallint         NOT NULL, -- 1 = buy, 2 = sell
    ema38           double precision NOT NULL,
    ema100          double precision NOT NULL,
    warmup          boolean          NOT NULL,
    PRIMARY KEY (symbol, window_start_ms)
);
