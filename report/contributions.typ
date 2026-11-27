= Contributions <sec:contributions>

In this section, each team member has specified their answers to the
questions stated in the project assignment description.

== Olli Nurminen <subsec:olli>

+ The most important decision about the system design was
  to use event-driven service architecure instead of monolithic application.
  Responsibilities are split into separate services with Kafka as the boundary
  between them. This is useful because each component has a different scaling
  and failure profile. A monolithic design would have been simpler to implement 
  but it would have tight coupling and a single part failing would affect the
  whole system.
+ The most challenging part was designing a correct event-time windowing.
  Events from different symbols are interleaved, may arrive out of order,
  and many symbols have long quiet periods. We implemented watermarks
  to decide when a 5-minute window is safe to close, a grace period for ordinary
  disorder, and a policy for genuinely late data. This was important to get right
  because wrong decisions effect EMA values and therefore the buy/sell signals.
+ My main contributions to the project were implementing the ingestion service
  for replaying the CSV files into Kafka with watermarks and pacing,
  the enginer service which runs the engine core per partition in a Kafka
  consumer group, and the sink, which writes the results to Postgres.
  I also added the Grafana dashboard, consumer lag metrics,
  built the experiment runner and the end-to-end check,
  and finally, run the experiments E1, E2, and E3.

  In my opinion our team was highly functional. We organized our work
  in meetings and in a Telegram group chat with active
  participation in discussion. We also made clearly defined issues
  in GitHub that allowed us to share the responsibilities and workload 
  effectively.

== Lassi Oinas <subsec:lassi>

+ The most important architectural decision was to split _breakout_ into
  individual services that each have a well-defined responsibility. This
  makes it possible to horizontally scale parts of the system that benefit
  from parallelization. Using Kafka as a buffer allows faster services
  to perform their operations without waiting for other, slower services.
  
  Another key decision that affected the design of our system was to use
  helper scripts to gain insights into how the source data behaves. For
  example, the `tools/datacheck/pricedrops` script made it clear that most tick events in the source
  data could already be dropped during ingestion, thus drastically
  reducing the load on system. The `tools/datacheck/order` script
  allowed us to verify that events of a given symbol are mostly in order,
  and we could thus confidently send price events to their partitions in
  the order they were parsed.

  A final significant decision was the usage of watermarks for tracking
  how much event time has passed. Watermarks allowed us to close windows,
  even if some symbols became inactive for longer periods of time.
+ For me, the most challenging part during system design was to formulate
  and iterate on a windowing and watermark mechanism that would correctly
  calculate query results, close windows in time, keep track of event
  time that has passed, and gracefully take late events into
  consideration. Furthermore, as our team decided to experiment with new
  technologies (e.g. Kafka, Go), it took some time for me to familiarize
  myself with the syntax and semantics of these technologies. A final
  challenge brought forth by the usage of AI was not to over-engineer
  our system design and understand the implementation that it produced.
+ I was actively involved in planning the system architecture from the
  start. This involved iterating on the designs until we felt that they
  would suit the needs of this project. After finalizing the architectural
  decisions, I documented them so that the rest of the team could refer
  to them. I also divided the system implementation into individual
  work items that contained all necessary information for completing
  them. In source code, I implemented the CSV parser for the ingestion
  service. I also worked on the engine core functionality that
  computes EMAs and generates advisories. I was also responsible for
  implementing parts of the data-checking scripts, since the results
  provided by them were essential when planning the system architecture.
  Finally, I set up the template for this project report and made sure
  that its appearance was uniform.

  I think our team worked well together. We were able to coordinate
  development efforts via Telegram and there were no hurdles when assigning
  work items to team members. We kept track of completed features via
  GitHub issues and communicated actively via Telegram when someone had
  finished a feature.

== Aaro Sulonen <subsec:aaro>

+ One of the main design decisions was to use REST for historical data and
  WebSocket for live updates. The API gets historical data from Postgres and live
  results directly from Kafka. This allows us to send results straight to the client,
  without waiting for them to be stored in the database. The clients can
  subscribe to the symbols they are interested in. We also generate TypeScript
  types from the shared Protobuf schemas to make sure that the backend and
  frontend use the same message formats.
+ The most challenging part of my implementation was combining historical and
  live data. New results are arriving while we are still fetching the historical data.
  The database might not be fully up to date with the livestream. To solve
  this, the client first subscribes to the live updates and waits for confirmation
  before fetching the historical data. During the request, we store the incoming
  live messages in a buffer and then merge the results based on the symbol, window
  start and message type. If the connection is lost, the client also needs to
  subscribe to the symbols and fetch the historical data again. I added tests to
  make sure that these situations are handled well.
+ My main contributions to the project were the API service and the React
  dashboard. I implemented the API endpoints for symbols, historical data and
  performance metrics, as well as the WebSocket functionality for subscribing to
  specific symbols. I also implemented the database queries for historical data
  and wrote tests for both the API and the database queries.

  In the frontend, I implemented the symbol search and selection, price and
  EMA charts, buy and sell advisories and the visualization of throughput and
  latency. I also included the frontend build in the API Docker image, added
  TypeScript type generation from Protobuf, integrated the API with Docker Compose
  and Prometheus.

  In my opinion, the division of work was quite good. Having separate services and
  GitHub issues made it clear what everyone was responsible for. The shared
  message schemas also made it easier to integrate the different services. I was
  able to focus on the API and frontend while the other group members worked on
  the data processing pipeline.
