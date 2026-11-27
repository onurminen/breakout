#import "@preview/faithful-acmart:0.2.0": *

#show: acmart.with(
  format: "sigconf",
  nonacm: true,
  title: "Detecting Trading Trends in Financial Tick Data",
  short-authors: "Nurminen, Oinas, Sulonen",
  authors: (
    (
      name: "Olli Nurminen",
      email: "olli.nurminen@aalto.fi",
      affiliation: (institution: "Aalto University", city: "Espoo", country: "Finland"),
    ),
    (
      name: "Lassi Oinas",
      email: "lassi.oinas@aalto.fi",
      affiliation: (institution: "Aalto University", city: "Espoo", country: "Finland"),
    ),
    (
      name: "Aaro Sulonen",
      email: "aaro.sulonen@aalto.fi",
      affiliation: (institution: "Aalto University", city: "Espoo", country: "Finland"),
    )
  ),
  abstract: include "abstract.typ",
)

// Only URLs: references and citations are links too
#show link: it => if type(it.dest) == str { text(fill: blue, it) } else { it }

#include "introduction.typ"
#include "architecture.typ"
#include "implementation.typ"
#include "performance.typ"
#include "contributions.typ"
#include "conclusion.typ"

#bibliography("refs.bib")

#include "appendix.typ"
