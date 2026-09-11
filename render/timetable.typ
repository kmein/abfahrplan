// the default is the file this document sits next to, which is what the
// embedded renderer writes; the Makefile points it at the repo root instead
#let timetable = json(sys.inputs.at("data", default: "timetable.json"))

// BVG house colours, sampled from BVG's own print PDFs (Linienverlauf 184, U6, M4)
#let bvg-yellow = rgb("#FDE103")
#let bvg-bus = rgb("#A0148E")
#let bvg-ubahn = rgb("#1565AF")
#let bvg-sbahn = rgb("#008D4F")
#let bvg-tram = rgb("#ED1C24")
#let bvg-rail = rgb("#555150")
#let bvg-ferry = rgb("#0B7285")
#let ink = rgb("#242021")
#let band = rgb("#ECEBEB")
#let quiet = rgb("#605D5E")
#let bvg-grey = rgb("#A5A3A4")
#let hairline = 0.4pt + ink

// The mode comes from the feed's route type, not from the line's name: M4 is a
// tram and M19 a bus, and 60 is a tram that used to come out bus-purple here.
#let lineColour(kind) = {
  if kind == "sbahn" { bvg-sbahn } else if kind == "ubahn" { bvg-ubahn } else if kind == "tram" {
    bvg-tram
  } else if kind == "rail" { bvg-rail } else if kind == "ferry" { bvg-ferry } else { bvg-bus }
}

#let showDirection(route, direction, kind) = {
  let badge = box(
    fill: lineColour(kind),
    inset: (x: 2.5pt, y: 1pt),
    outset: (y: 1pt),
    radius: 1pt,
    text(fill: white, weight: 700, size: 7pt, route),
  )
  let arrow = text(weight: 700, if direction == 0 { "→" } else { "←" })
  [#badge#h(2.5pt)#arrow]
}

// the weekdays a departure is limited to, abbreviated as on a German
// timetable; the JSON names the weekdays it is missing from instead
#let showExcludedWeekdays(departure) = {
  let excluded = departure.at("excluded_weekdays", default: ())
  if excluded.len() == 0 { return [] }
  let short = (Mon: "Mo", Tue: "Di", Wed: "Mi", Thu: "Do", Fri: "Fr")
  let served = ("Mon", "Tue", "Wed", "Thu", "Fri").filter(day => not excluded.contains(day))
  super(text(fill: bvg-bus, weight: 700, served.map(day => short.at(day)).join(",")))
}

#let showDepartures(departures) = {
  let showOne(departure) = [#if departure.minute < 10 [0]#str(departure.minute)#showExcludedWeekdays(departure)]
  departures.map(showOne).join(" ")
}

#set page(
  paper: "a4",
  margin: (left: 1.3cm, right: 0.8cm, top: 0.8cm, bottom: 1cm),
  background: place(top + left, rect(width: 0.5cm, height: 100%, fill: bvg-bus)),
  footer: {
    set text(size: 6.5pt, fill: quiet)
    grid(
      columns: (1fr, auto),
      [Alle Angaben ohne Gewähr. Erzeugt aus den VBB-GTFS-Daten.],
      context counter(page).display(),
    )
  },
)
#set text(font: "Fira Sans", size: 8pt, fill: ink, number-width: "tabular")
#set table(stroke: none)

#let legend = (:)
#for hour in timetable.hours {
  for direction in hour.directions {
    let key = direction.route_short + "|" + str(direction.direction) + "|" + direction.at("kind", default: "bus")
    let departures = (direction.departuresMonFri, direction.departuresSat, direction.departuresSun).flatten()
    let headsigns = departures.map(departure => departure.headsign)
    legend.insert(key, (legend.at(key, default: ()) + headsigns).sorted().dedup())
  }
}

#block(fill: band, width: 100%, inset: (x: 7pt, y: 6pt), below: 8pt)[
  #grid(
    columns: (auto, 1fr),
    column-gutter: 7pt,
    align: horizon,
    box(
      fill: bvg-yellow,
      radius: 100%,
      width: 22pt,
      height: 22pt,
      align(center + horizon, text(fill: bvg-bus, weight: 800, size: 13pt)[H]),
    ),
    [
      #text(size: 15pt, weight: 800, timetable.station)
      #h(5pt)
      #text(size: 8pt, weight: 600, fill: quiet)[Abfahrten]
      #h(1fr)
      #text(size: 7pt, fill: quiet)[Stand: #datetime.today().display("[day].[month].[year]")]
    ],
  )
]

#let headerCell(body, fill: band) = table.cell(fill: fill, text(weight: 800, size: 7.5pt, body))

#let hourRows(hour) = {
  let byRoute = hour.directions.sorted(key: direction => direction.route_short + str(direction.direction))
  let cells = byRoute.map(direction => (
    showDirection(direction.route_short, direction.direction, direction.at("kind", default: "bus")),
    showDepartures(direction.departuresMonFri),
    showDepartures(direction.departuresSat),
    showDepartures(direction.departuresSun),
  )).flatten()
  let hourCell = table.cell(rowspan: hour.directions.len(), fill: band, text(weight: 800, size: 11pt, str(hour.hour)))
  (table.hline(stroke: hairline), hourCell) + cells
}

#block(width: 100%, below: 7pt)[
  #set text(size: 6.5pt)
  #text(weight: 700)[Fahrtziele:]#h(4pt)
  #for (key, headsigns) in legend {
    let parts = key.split("|")
    [#showDirection(parts.at(0), int(parts.at(1)), parts.at(2)) #headsigns.join(", ") #h(7pt)]
  }
]

#let longestRun = calc.max(..timetable.hours.map(hour => calc.max(..hour.directions.map(direction => calc.max(
  direction.departuresMonFri.len(),
  direction.departuresSat.len(),
  direction.departuresSun.len(),
)))))

#columns(if longestRun > 8 { 1 } else { 2 }, gutter: 10pt, table(
  columns: (auto, auto, 1fr, 1fr, 1fr),
  inset: (x: 3pt, y: 2pt),
  align: (right + horizon, left, left, left, left),
  table.vline(x: 2, stroke: 0.3pt + bvg-grey),
  table.vline(x: 3, stroke: 0.3pt + bvg-grey),
  table.vline(x: 4, stroke: 0.3pt + bvg-grey),
  table.header(
    headerCell[],
    headerCell[Linie],
    headerCell(fill: bvg-yellow)[Mo–Fr],
    headerCell(fill: bvg-yellow)[Sa],
    headerCell(fill: bvg-yellow)[So],
  ),
  ..timetable.hours.map(hourRows).flatten(),
  table.hline(stroke: hairline),
))
