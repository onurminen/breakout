= Introduction

Modern financial markets produce vast amounts of data related to
financial instruments such as individual stocks, exchange-traded funds
(ETFs) or market indices (e.g. the S&P 500 index). Put simply, the
essence of trading consists of combining this data with broad knowledge
about the behaviour of financial instruments, recent economic,
geopolitical, and social events, as well as reliable information
about the anticipated performance of a firm or market segment.
Gathering, analyzing, and interpreting this information allows market
participants to make informed decisions regarding which types of trades
they should and shouldn't engage in.

Granular market data consists of a continuous stream of _tick
events_. An individual tick event usually contains the following 
information:
- Financial instrument (_symbol_) that the event describes. The symbol
  is a string such as "SPX".
- Either the latest buy or sell price of that event, depending on the 
  type of the event. If the event describes a sell event, then it 
  contains the sell (bid) price of that event.
- Trading volume of an event, which describes how many shares were 
  bought
  at a given price, for example.
- Other metadata such as trade dates and timestamps.

The goal of this course project is to utilize real-world tick data and
compute useful indicators based on it. These indicators 
can be utilized by traders to identify attractive trading 
opportunities. 
In this project, for each symbol, we are interested in two main 
"queries":
+ Exponential moving averages (EMAs) over 5-minute tumbling windows.  
+ Crossovers of two different EMAs of a given symbol, called _breakout 
  patterns_.
The computation details for these queries are explained in section
TODO.

The project dataset consists of the DEBS 2022 Grand Challenge Data
Set@debs2022data, that contains tick data captured by the systems of 
Infront Financial Technology GmbH during the week from November 8th until 
November 14th, 2021. The data is available as CSV-files that 
altogether contain 289 million tick data events, covering 5504 
equities and indices traded across three European exchanges: Paris 
(FR), Amsterdam (NL), and Frankfurt (ETR). The dataset contains an 
abundance of  attributes for each event, but for the purposes of this 
project, we  are only interested in the following attributes:

#figure(
  placement: none,
  table(
    columns: 3,
    align: left,
    stroke: (x, y) => (
      left: if x > 0 { 0.5pt },
      top: if y == 1 { 0.5pt },
    ),
    table.header[*Attribute*][*Example*][*Description*],
    [`ID`], [`PX1.FR`], [Symbol, including the exchange suffix],
    [`SecType`], [`I`], [Security type: either equity (`E`) or index (`I`)],
    [`Date`], [`08-11-2021`], [Day of the event],
    [`Last`], [`7046.170000`], [Last trade price],
    [`Trading time`], [`09:11:45.472`], [Timestamp of the event],
  ),
  caption: [Dataset attributes used by our system],
)

Note that contrary to the project assignment description, we use `Date`
instead of `Trading date`. We used a data-checking script to identify
that for valid price events, approximately 90% of the values for the
`Trading date` column are empty across all data files. Consequently, we
decided to use `Date` instead of `Trading date` when parsing and
constructing event timestamps. The data-checking script is located in
`tools/datacheck/tradingdate`. More information about helper scripts
that inspect the source data is provided in section TODO.

This project focuses on designing and implementing a system capable
of processing high-volume data in an efficient manner. The goal is to
produce correct outputs while maintaining a high level of throughput
and low latencies. Furthermore, this project aims to visualize results
in a way that would benefit a hypothetical market participant.

This project report is structured as follows (TODO: add refs). Section
2 documents the system architecture. Section 3 describes how this
architecture was implemented in practice and which technologies were
used. Section 4 evaluates the performance of our system under different
loads and resource constraints. Section 5 details the project
contributions of each team member, and section 6 concludes this project
report.
