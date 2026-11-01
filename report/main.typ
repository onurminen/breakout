#import "@preview/faithful-acmart:0.2.0": *

#show: acmart.with(
  format: "sigconf",
  nonacm: true,
  title: "Detecting Trading Trends in Financial Tick Data",
  short-authors: "Oinas, Surname 2, Surname 3",
  authors: (
    (
      name: "Aaro Sulonen",
      email: "aaro.sulonen@aalto.fi",
      affiliation: (institution: "Aalto University", city: "Espoo", country: "Finland"),
    ),
    (
      name: "Lassi Oinas",
      email: "lassi.oinas@aalto.fi",
      affiliation: (institution: "Aalto University", city: "Espoo", country: "Finland"),
    ),
    (
      name: "Olli Nurminen",
      email: "olli.nurminen@aalto.fi",
      affiliation: (institution: "Aalto University", city: "Espoo", country: "Finland"),
    ),
  ),
  abstract: lorem(120),
)

// Sections follow the report organization in the assignment description, Section 4.1.

= Introduction
#lorem(180)

#lorem(120)

= System Architecture
#lorem(150)

#figure(
  rect(width: 100%, height: 4cm, stroke: 0.5pt)[Architecture diagram],
  caption: [Lorem ipsum dolor sit amet.],
) <architecture>

== Ingestion
#lorem(140)

== Engine
#lorem(160)

== Sink, API and UI
#lorem(120)

= Implementation
#lorem(160)

#figure(
  tabular(
    columns: 2,
    toprule(),
    [Concern], [Choice],
    midrule(),
    [Language], [Go],
    [Broker], [Apache Kafka],
    [Database], [Postgres],
    bottomrule(),
  ),
  caption: [Lorem ipsum dolor sit amet.],
) <technology>

#lorem(120)

= Performance Evaluation
#lorem(100)

== Correctness
#lorem(140)

== Scalability
#lorem(160)

#figure(
  rect(width: 100%, height: 4cm, stroke: 0.5pt)[Throughput plot],
  caption: [Lorem ipsum dolor sit amet.],
) <scalability>

== Resource Utilization
#lorem(140)

= Conclusion
#lorem(120)

= Contributions
// The assignment requires each group member's contributions.
#lorem(60)

The data set is described in @debs2022data.

#bibliography("refs.bib")
