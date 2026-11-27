= Conclusion <sec:conclusion>

The system calculates five-minute EMA38 and EMA100 values and
generates buy and sell advisories based on financial tick data. Kafka
partitioning based on symbols makes it possible to process different symbols
independently. Event-time watermarks and a one-second grace period are used to
handle events that arrive out of order. Historical results are stored in
Postgres, and both historical and live results can be accessed through the API
and web dashboard.

The correctness tests produced 1,855,645 EMA results for the full week of data.
In the tests, we compared the stored results and advisories with the engine-
core reference run for twelve selected symbols. Based on these tests, the
processing logic and its integration with Kafka and the database work as
expected for the tested data.

The performance tests show that the engine can process data much faster than the
live pipeline receives it. When using a pre-filled Kafka topic, the throughput
increased from 3.62 million events per second with one engine to 5.58 million
with eight engines. The performance improvements became smaller as we got closer
to the CPU limits of the machine. The live pipeline reached around 235,000
events per second, with data ingestion being the bottleneck. Both measured
latency metrics stayed below 20 ms at the 99th percentile for all tested replay
speeds.

