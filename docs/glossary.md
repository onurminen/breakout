# Glossary

| Term | Meaning |
|---|---|
| **Symbol** | One instrument on one exchange, e.g. `RDSA.NL` (Royal Dutch Shell in Amsterdam). The suffix is the exchange: `.FR` Paris, `.NL` Amsterdam, `.ETR` Frankfurt/Xetra |
| **Equity / index** | A company's stock (`SecType` `E`) or a stock-market index such as the CAC 40 (`I`) |
| **Price event** | A CSV line with a filled `Last` column. About 22% of lines |
| **Housekeeping event** | Any other line, e.g. a quote or status update. Dropped by ingestion |
| **Event time** | When something happened in the market. Here: `Date` + `Trading time` |
| **Arrival order** | The order in which events reach the system, i.e. their order in the file |
| **Window** | A 5-minute slice of event time, 00:00–00:05, 00:05–00:10, and so on. Windows never overlap ("tumbling windows") |
| **EMA** | Exponential moving average: each window's price moves the average a fixed fraction towards itself, 2/39 for EMA38 and 2/101 for EMA100. EMA38 reacts faster |
| **Crossover** | EMA38 moving from below EMA100 to above it (**bullish**, buy advisory) or from above to below (**bearish**, sell advisory) |
| **Advisory** | The buy or sell message the system sends on a crossover |
| **Warm-up** | The period after the start when the EMAs, starting from 0, have not yet caught up with the real price. The first window's buy advisory is an artifact of it |
| **Watermark** | A message from ingestion saying "everything up to event time T has been sent". The engine closes windows based on it |
| **Grace period** | Extra time after a window's end before it closes, so slightly out-of-order events still count. 1 second |
| **Late event** | An event that arrives after its window has closed. Counted and dropped |
| **Kafka topic** | A named stream of messages, e.g. `ticks` |
| **Partition** | One of the ordered logs a topic is split into. Order is guaranteed only inside a partition. Each partition is read by one engine instance |
| **Consumer group** | Several instances of a service sharing one name. Kafka divides the partitions among them |
| **Offset** | A message's position in its partition. A service saves its offset to resume after a restart |
| **Consumer lag** | How many messages a service is behind the end of its partition |
| **Idempotent write** | A write that can be repeated without changing the result, so a retried batch causes no duplicates |
| **Protobuf** | A compact binary message format with a schema, from which code for Go and TypeScript is generated |
| **p50 / p95 / p99** | Latency percentiles: 50%, 95% or 99% of measurements are at or below this value |
