#set page(columns: 1, flipped: true)

= Appendix <sec:appendix>

== Architecture Diagram <subsec:architecture-diagram>

#figure(
  image("architecture-diagram.pdf", width: 100%),
  placement: none,
  caption: [System architecture of _breakout_],
) <figure:architecture>

== EMA & Breakout Pattern Calculation Formulas <subsec:formulas>

For reference, we restate the definitions for the EMA as well as
for bullish and bearish breakout patterns, as given in the project
assignment description. In the following, we have

- $w_i$ denotes a specific 5-minute window with identifier $i$
- $j$ is the EMA smoothing factor with $j in {38, 100}$
- $s$ denotes the symbol that the event describes
- $"Close"_(s,w_i)$ is the last price observed for $s$ in window $w_i$
- $"EMA"_(s,w_0)^j = 0$

// Remove first line indent since it looked silly
#par(first-line-indent: 0pt)[The EMA of symbol $s$ in window $w_i$ is]

$ "EMA"_(s,w_i)^j = "Close"_(s,w_i) dot 2/(1+j)
  + "EMA"_(s,w_(i-1))^j dot (1 - 2/(1+j)) $ <eq:ema>

A bullish breakout pattern, which creates a buy advisory, is observed
if and only if

$ "EMA"_(s,w_i)^38 > "EMA"_(s,w_i)^100 quad "and" quad
  "EMA"_(s,w_(i-1))^38 <= "EMA"_(s,w_(i-1))^100 $ <eq:bullish>

A bearish breakout pattern, which creates a sell advisory, is observed
if and only if

$ "EMA"_(s,w_i)^38 < "EMA"_(s,w_i)^100 quad "and" quad
  "EMA"_(s,w_(i-1))^38 >= "EMA"_(s,w_(i-1))^100 $ <eq:bearish>
