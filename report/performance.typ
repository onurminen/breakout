= Performance Evaluation
All measurements were ran locally on a PC with 
AMD Ryzen 5 2600x with 12 cores @ 4.25GHz and 15.54 GiB of memory
running Arch Linux operating system.

All measurements were collected locally on a single desktop computer
with and AMD Ryzen 5 2600X
processor with 6 physical CPU cores and 12 logical CPUs, running at up
to 4.25 GHz and 15.5 GiB of memory, under Arch Linux operating system.

CPU usage in this report is expressed in logical-CPU equivalents:
1.0 CPU corresponds to one CPU-second of execution per second of
wall-clock time. Docker limits use the same unit and do not reserve
specific physical cores.

== Correctness
We test correctness on three levels.
Firstly, we have a wide coverage of unit tests for each component of
the system. The engine core responsible for the computations 
has tests for EMA calculations, buy/sell crossovers, near-equal EMAs,
empty windows, late/out-of-order events, midnight boundaries,
invalid prices and first-window warm-up case.

Secondly, we have a full-week reference run without Kafka.
It feeds all seven data files directly through the engine core
to prove the processing logic against the raw data without
distributed-system components affecting the result.
It verifies the expected 1,855,645 EMA results and 
5,506 late events that match the independently counted non-empty
windows and the known anomalous Wednesday 01:00 events.
The full pipeline gives the same totals with one and four engine
instances, and the database still holds 1,855,645 rows after
the sink was killed and restarted during a run.

Thirdly, an end-to-end database comparison. 
The Docker Compose pipeline replays the entire week for 12 
representative symbols. After the sink writes results to Postgres,
enginecheck independently computes the expected results again
and compares both database tables row by row. 
This catches errors in serialization, topic partitioning,
consumer behavior, persistence, and cross-day EMA state.

== Scalability
We performed three experiments with different purposes. 
Each experiment tests multiple configurations and each 
configuration was ran three times. We use the median results
of the three runs in this report.

*Experiment 1: engine scaleout.*
We measured engine throughput, CPU use, CPU-seconds, and peak memory
usage with different number of engine instances. The goal was to find
out whether adding more engine replicas makes the processing faster.
The experiment pre-fills Kafka with the whole week to remove ingestion
from the equation. This way the raw processing capacity of the engine
is measured.
The results are shown in table <engine-results1>.
A single engine processes 3.62 million events per second and its
goroutines spread across about seven cores. Two instances reach
5 million and beyond that the throughput levels off at 5.5 million
events per second with the engines using about 10 of the 12 cores.

#figure(
  tabular(
    columns: 5,
    toprule(),
    [Setting], [Events/s], [Cores], [CPU-s], [Memory],
    midrule(),
    [1 engine], [3.62M], [6.9], [111], [0.6 GB],
    [2 engines], [5.01M], [9.9], [114], [1.0 GB],
    [4 engines], [5.44M], [10.3], [111], [1.9 GB],
    [8 engines], [5.58M], [10.3], [108], [2.5 GB],
    bottomrule(),
  ),
  caption: [Engine throughput on the filled topic. 1-8 
  engine instances without CPU limit. Cores and CPU-seconds are
  summed over the engine. Memory is the peak of all engines together],
) <engine-results1>

*Experiment 2: CPU budget.* The second experiment measures 
how does a single engine scale with more CPU cores.
It is given 0.5, 1, 2, and 4 cores and the same metrics are measured
as in experiment 1. The results are shown in table <engine-results2>.
Throughput grows almost linearly at about 0.75M events/s per core up 
to two cores and 0.64M per core at four. A partition per goroutine
thus spreads the worker over the available cores with little
contention.

#figure(
  tabular(
    columns: 5,
    toprule(),
    [Setting], [Events/s], [Cores], [CPU-s], [Memory],
    midrule(),
    [0.5 CPU], [0.38M], [0.50], [78], [0.5 GB],
    [1 CPU], [0.73M], [1.01], [82], [0.5 GB],
    [2 CPUs], [1.49M], [1.99], [77], [0.5 GB],
    [4 CPUs], [2.56M], [3.96], [84], [0.5 GB],
    bottomrule(),
  ),
  caption: [Engine throughput on the filled topic. Top: E1, 1–8 engine instances without CPU limit. Bottom: E2, one engine with a CPU limit. Cores and CPU-seconds are summed over the engines; memory is the peak of all engines together.],
) <engine-results2>

*Experiment 3: replay speed.* 
The third experiment tests can the complete live pipeline keep
latency low as the input rate rises. Ingestion replays Monday's data
at real time, 10 and 60 event-time seconds per real second,
and as fast as it can, with one engine and the sink.
Paced runs replay the night quickly as there is not many
events at night time and then measure 10 minutes from 09:00.
For max run the whole file is sent as fast as possible.
The results in table <latency-results> show throughput,
event latency, and window-result latency.
As fast as possible, the whole pipeline processes about
235,000 events per second, limited by ingestion, which is single
threaded. The 99th-percentile latencies stay below 20 ms at every
speed. The median event latency falls from 10.9 ms at 1x to 8.1 ms
at 60x, while the 95th-percentile stays at 13 ms.
Because the Kafka producer waits up to 10 ms to fill a batch,
at 1x almost every event starts a new batch, while at higher
rates events join batches that are already waiting.
At 1x only a few windows close in ten minutes, so window latency
varies between runs (median 4.1-14.5 ms).

#figure(
  tabular(
    columns: 4,
    toprule(),
    [Speed], [Events/s], [Event latency], [Window latency],
    midrule(),
    [1×], [397], [10.9 / 13.0 / 13.2], [7.8 / 15.1 / 15.5],
    [10×], [3.6k], [8.8 / 12.9 / 13.2], [11.8 / 15.3 / 15.6],
    [60×], [18.5k], [8.1 / 12.9 / 14.3], [8.9 / 14.4 / 15.4],
    [max], [235k], [9.7 / 15.1 / 17.3], [10.9 / 15.8 / 18.0],
    bottomrule(),
  ),
  caption: [E3: live throughput and latenct (p50 / p95 / p99, in ms)
  at different replay speeds, one engine],
) <latency-results>

== Resource utilization
Processing the whole week costs the engines about
110 CPU-seconds in E1, independent of the number
of instances: additional instances cost almost no CPU, 
but each needs its own memory, so memory grows from 0.6 GB 
for one engine to 2.5 GB for eight. With a CPU limit (E2), 
the same work takes only 77–84 CPU-seconds. An engine
without a limit spreads over about seven cores and 
spends more CPU on the same work, presumably in Go's 
scheduler and garbage collector, but we have not verified this. 
For a fixed CPU budget, a limited engine is thus the more 
efficient choice, while an unlimited one finishes sooner.

In the live pipeline (E3), the services use little CPU at real-time 
speed, at 1×, ingestion and the engine use about 0.02 cores each, 
while Kafka needs about 0.5 cores regardless of the load. At 60×, 
ingestion uses 0.15 cores and the engine 0.12. As fast as possible, 
ingestion uses 1.1 cores, the engine 0.7, Kafka 0.85 and the sink 
less than 0.02. Ingestion is the bottleneck, its parser alone
handles about 870,000 events/s on one core, so most of its time 
goes into encoding and sending events, not parsing. The engine 
on its own would need less than one core for the rate that 
ingestion can deliver.
