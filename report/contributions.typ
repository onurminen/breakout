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
