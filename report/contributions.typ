= Contributions <sec:contributions>

Each team member must answer the following questions as individual contributions in the report:
1. What are the most important decisions you and your 
group have made about the system design?
2. What are the most challenging parts during 
the design and implementation?
3. What is your specific contribution in the team? 
How do you characterize the team’s overall func-
tionality?

== Olli Nurminen <subsec:olli>

(1) The most important decision about the system design was
to use event-driven service architecure instead of monolithic application.
Responsibilities are split into separate services with Kafka as the boundary
between them. This is useful because each component has a different scaling
and failure profile. A monolithic design would have been simpler to implement 
but it would have tight coupling and a single part failing would affect the
whole system.

(2) The most challenging part was designing a correct event-time windowing.
Events from different symbols are interleaved, may arrive out of order,
and many symbols have long quiet periods. We implemented watermarks
to decide when a 5-minute window is safe to close, a grace period for ordinary
disorder, and a policy for genuinely late data. This was important to get right
because wrong decisions effect EMA values and therefore the buy/sell signals.

(3) My main contributions to the project were implementing the ingestion service
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

== Aaro Sulonen <subsec:aaro>

(1) One of the main design decisions was to use REST for historical data and
WebSocket for live updates. The API gets historical data from Postgres and live
results directly from Kafka. This allows us to send results straight to the client,
without waiting for them to be stored in the database. The clients can
subscribe to the symbols they are interested in. We also generate TypeScript
types from the shared Protobuf schemas to make sure that the backend and
frontend use the same message formats.

(2) The most challenging part of my implementation was combining historical and
live data. New results are arriving while we are still fetching the historical data.
The database might not be fully up to date with the livestream. To solve
this, the client first subscribes to the live updates and waits for confirmation
before fetching the historical data. During the request, we store the incoming
live messages in a buffer and then merge the results based on the symbol, window
start and message type. If the connection is lost, the client also needs to
subscribe to the symbols and fetch the historical data again. I added tests to
make sure that these situations are handled well.

(3) My main contributions to the project were the API service and the React
dashboard. I implemented the API endpoints for symbols, historical data and
performance metrics, as well as the WebSocket functionality for subscribing to
specific symbols. I also implemented the database queries for historical data
and wrote tests for both the API and the database queries.

I also did some frontend work. I implemented the symbol search and selection, price and
EMA charts, buy and sell advisories and the visualization of throughput and
latency. I also included the frontend build in the API Docker image, added
TypeScript type generation from Protobuf, integrated the API with Docker Compose
and Prometheus.

In my opinion, the division of work was quite good. Having separate services and
GitHub issues made it clear what everyone was responsible for. The shared
message schemas also made it easier to integrate the different services. I was
able to focus on the API and frontend while the other group members worked on
the data processing pipeline.
