#import "@preview/faithful-acmart:0.2.0": tabular, toprule, midrule, bottomrule

= System Architecture <sec:architecture>

This section documents the high-level system architecture of _breakout_.
We aim to provide an overview of the different components that _breakout_
consists of. Furthermore, this section discusses implementation decisions
regarding topic partitioning and symbol state management, among others.
We also aim to justify these decisions as well as the system architecture.
@sec:implementation then focuses on how the components outlined in this
section were implemented in practice.

The system architecture of _breakout_ is inherently modular. The
application functionality spreads across different services that are
each responsible for a given task. We chose to design _breakout_ as
a collection of modular services for a multitude of reasons. Firstly,
the modular architecture decouples individual services. This means
that the performance and correctness of each component can be analyzed
individually. Secondly, the architecture allows individual services to
be scaled horizontally, if the need arises. In production deployments,
it would thus be possible to scale services that bottleneck the system.
Finally, development efforts are easier to coordinate, when services
are loosely coupled. Changes to a service only need to ensure that its
interface with other services is kept up-to-date. This also allows
swapping out a service entirely, e.g. replacing our Go API with a
TypeScript API.

The architecture of _breakout_ is visualized in @figure:architecture found
in the appendix. We introduce each component briefly below, so that
terminology used in the following subsections is familiar.
+ *Ingestion.* Parses CSV files and produces price events.
+ *Engine.* Computes EMAs, buy/sell advisories and publishes them.
+ *Sink.* Reads computed EMAs and advisories and stores them in
  persistent storage.
+ *API and UI.* Serve and display EMA calculation results and
  advisories, respectively.

== Ingestion <subsec:ingestion>

The ingestion service is responsible for parsing a single line of a CSV
file into a tick event. The ingestion service already provides a first
possibility for optimization: Since we are only interested in "valid" tick
events, i.e. events that have a non-null `Last` attribute, the ingestion
service drops events where `Last` is null or below 0. The performance
gain that is obtained by this is significant, although we have not
measured it explicitly. In total, approximately 79% of tick events are
invalid, and are thus dropped. We measured this with a data-check script
found in `tools/datacheck/pricedrops`. A valid tick event becomes a
`PriceEvent`, which contains the symbol, security type, price and
timestamp of the event.

After successfully parsing a `PriceEvent`, it is sent to a Kafka topic
titled `ticks`, which is divided into 12 partitions. We chose 12
partitions specifically, since this allows us to experiment with different
amounts of engines while keeping the amount of partitions assigned to each
engine equal. For example, with 4 engines, each engine would receive 3
partitions. The scaling of engines and how they consume tick events from
`ticks` is detailed in @subsubsec:engine-scaling.

Price events are distributed across the partitions based on a hash of
their symbol. In pseudocode, the partition number is derived from
`hash(symbol) % 12`. This means that each price event of a given symbol
always lands in the same partition. This heuristic of dividing events to
partitions based on their symbol is simple, but it results in uneven
partition sizes when the distribution of events across symbols is
not uniform.

We have now established how CSV lines are filtered, transformed into
price events, and how those events are distributed across partitions
of a Kafka topic. However, for an engine to compute EMA results and
advisories, it needs to know when it can mark a window as closed. For
this purpose, in addition to price events, the ingestion service also
publishes _watermarks_ to each partition of the `ticks` topic. A
watermark is a timestamp $T$ that asserts that all events up until $T$
have been ingested. Thus, if the engine is currently computing the EMA
for a window $w_i$ ending at 15:30, and receives a watermark with $T =$
15:31, it knows to close $w_i$. Watermarks are published each time a
second of event time has passed. More details regarding the heuristics
of closing windows are provided in @subsubsec:grace-period-late-events.

== Engine <subsec:engine>

The engine service is responsible for computing EMA results and generating
buy/sell advisories based on these results. It publishes results and
advisories to their corresponding Kafka topics `ema-results` and
`advisories`. The design of the engine service entails multiple decisions,
each of which are explained in the sections below.

=== Symbol Independence <subsubsec:symbol-independence>

A key insight is that EMA results of symbols are independent of each
other. For a given symbol, the current EMA thus only depends on the EMA
of the previous window, as well as the latest observed price of the
current window. Consequently, the computation of EMAs and advisories can
be spread across multiple engine instances, as long as all events of a
symbol are processed by the same instance. This is achieved by using the
symbol as a partition key, as explained in @subsec:ingestion.
@subsubsec:engine-scaling outlines how we can exploit this independence.

=== Event Arrival Order <subsubsec:arrival-order>

When computing the EMA of a given symbol, all price events of that symbol
should ideally arrive at the engine in the order of their event time. In
other words, when an engine receives a watermark $T$, it should ideally
not receive any more events whose timestamp is in the interval $[0, T]$.
We wanted to measure how ordered the source data is with regards to this
property. The `tools/datacheck/order` script is used to analyze how many
events arrive later than their event time. We found that, within each
symbol, all of their events arrive in the correct order. The only
exception are 5506 index events on Wednesday, all with trading time
01:00, which arrive approximately 18 hours after the window they would
belong to. The following section discusses how late events are handled,
and how we characterize an event as being late.

=== Grace Period & Late Events <subsubsec:grace-period-late-events>

Even though events may (mostly) arrive in the correct order, it might be that
some events belonging to window $w_t$ might arrive only after this window
has closed. Thus, we wanted to measure the amount of such delayed events,
and how significant the delays were. We measured this with the
`tools/datacheck/grace` script. The script introduces the concept of a
_grace period_, which indicates how long an engine "waits" for delayed
events to include them in the calculations of the window that would have
otherwise been closed already. The table below presents the results.

#figure(
  tabular(
    align: left,
    columns: 6,
    toprule(),
    [Day], [0 s], [1 s], [10 s], [1 min], [5 min],
    midrule(),
    [Mon 08-11], [79], [0], [0], [0], [0],
    [Tue 09-11], [76], [0], [0], [0], [0],
    [Wed 10-11], [5,560], [5,506], [5,506], [5,506], [5,506],
    [Thu 11-11], [119], [0], [0], [0], [0],
    [Fri 12-11], [24], [0], [0], [0], [0],
    bottomrule(),
  ),
  placement: none,
  caption: [Late price events per grace period],
) <table:grace>

As expected, no reasonable grace period can take Wednesday's anomalies
into account, and therefore those price events are simply dropped. For
all other days, we notice that with a grace period of 1 second, all late
events are included in the windows they belong to. Consequently, we
choose a grace period of 1 second, meaning that a window from 15:05 to
15:10 stays open until the watermark reaches 15:10:01. Until then, it
takes into account all events whose event time falls within the window,
even if they arrive after the watermark has passed 15:10. Note that
_arrival time_ should not be confused with _event time_. An event with
event timestamp 15:12 still gets assigned to window $[$15:10, 15:15$]$.

Waiting for late events introduces a compromise between the accuracy and
the delay of EMA results. With a grace period of 1 second, the EMA result
for a symbol is computed 1 second after the window has closed, introducing
a delay. On the other hand, the result is more accurate, since it takes
the latest events belonging to a window into account. We chose to prioritize
result accuracy, since the last price events of a window could have a
significant impact on its EMA. If our data checks would have shown that a
significantly larger grace period would have been necessary to include
late events, it would have made more sense to prioritize result speed
instead.

=== Maintaining EMA State <subsubsec:ema-state>

For simplicity, let us consider computing the $"EMA"_(s,w_t)^38$ of window
$w_t$ for symbol $s$. In accordance with @eq:ema, to compute
$"EMA"_(s,w_t)^38$, we need the closing price of window $w_t$ and the
EMA of the previous window. Thus, an engine computing $"EMA"_(s,w_t)^(38)$
needs to maintain the following states: 1) $"EMA"_(s,w_(t-1))^(38)$,
2) the latest price $p_s$ it has observed for $s$, and 3) the current
watermark indicating the latest timestamp until which all events have
been published by the ingestion service. When the engine consumes price
events from its partition, it needs to continuously update $p_s$. Upon
receiving a watermark that closes the open window, the engine computes
$"EMA"_(s,w_t)^(38)$ with the latest price of the window, and publishes
it to `ema-results`. Furthermore, it compares the obtained result with
$"EMA"_(s,w_t)^(100)$, and publishes an advisory if the conditions
indicated in @eq:bullish or @eq:bearish are met. Computation for other
symbols and smoothing factors is done analogously, such that the states
are stored in maps keyed by their symbols.

=== Engine Scaling <subsubsec:engine-scaling>

Since EMA calculations are independent between symbols, we
can utilize multiple engines to compute them in parallel. For this, the
12 Kafka partitions of `ticks` are divided among the engine instances.
Continuing from our previous example, with 4 engine instances, each
instance would receive 3 partitions. The instance then spawns 3
`goroutines` that each read a single partition. Each of these `goroutines`
starts with an empty engine state, and independently computes and
publishes EMA results and advisories based on the events in its assigned
partition. Thus each `goroutine` maintains its state as explained in
@subsubsec:ema-state. The performance metrics of scaling engines are
documented in @sec:performance.

== Sink <subsec:sink>

The sink service is used to store computed EMA results and advisories
in a database. We do this since clients also need to fetch the history
of how an EMA has evolved over time. A deliberate design decision here
was to have the sink as a separate service from the engine. The engine
could also write its results to the database directly, with no sink in
between. However, we wanted to implement a separate sink service to
prevent that the engine is bottlenecked by slow database transactions.
Furthermore, the sink service allows an engine to recover from a crash.
The sink service is idempotent: If an engine restarts and starts
recalculating EMA results, the sink service does not write duplicate
values to the database. The sink also prevents history gaps by writing
all results to the database, even if an engine has crashed in between.

== API & UI <subsec:api-ui>

The API and UI are responsible for transporting and visualizing EMA
results and advisories to the user. For live results, a WebSocket
connection is established between the client and the server. The server
reads results from Kafka, and directly forwards them to the client.
The clients receive live updates from the symbols they are subscribed to.
The history is served via a separate endpoint, and the client is
responsible for merging live and history data. Thus no gaps appear in
the live and history data.

We chose to implement a WebSocket connection for live data, since it is
crucial to deliver live results to the client as quickly as possible.
Reading live results from the database would have introduced an
additional delay. For history data, this is not an issue, since it is
not latency-critical.
