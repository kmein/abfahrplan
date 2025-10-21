#let timetable = json("timetable.json")

#let showDirection(x) = {
  let directionString = if x.direction == 0 {
    "→"
  } else {
    "←"
  }
  x.route_short + directionString
}

#let showExcludedWeekdays(departures) = {
  let weekdays = departures.at("excluded_weekdays", default: ())
  if weekdays.len() == 0 []
  else {
    let allWeekdays = ("Mon", "Tue", "Wed", "Thu", "Fri")
    let excluded = allWeekdays.filter(day => not (weekdays.contains(day)))
    super(excluded.join(","))
  }
}

#let showDepartures(departures) = {
  departures.map(x => str(x.minute) + showExcludedWeekdays(x)).join(" ")
}

#set page(margin: 1cm)
#set text(9pt, font: "Alegreya Sans")
#set table(stroke: none)


#let directionMap = (:)
#for hour in timetable.hours {
  for direction in hour.directions {
    let directionString = showDirection(direction)
    if not (directionString in directionMap) {
      directionMap.insert(directionString, ())
    } else {
      let headsigns = (directionMap.at(directionString, default: ()) + (direction.departuresMonFri, direction.departuresSat, direction.departuresSun).flatten().map(x => x.headsign)).sorted().dedup()
      directionMap.insert(directionString, headsigns)
    }
  }
}


#grid(columns: (auto, auto),
  columns(2, gutter: 0cm,
  table(
    columns: 5,
    table.header("", "Line", "Mo-Fr", "Sa", "Su"),
    ..timetable.hours.map(x =>
      (table.hline(start: 1), table.cell(rowspan: x.directions.len(), strong(str(x.hour))),) +
      x.directions.sorted(key: (d) => d.route_short + str(d.direction)).map(direction => (
        showDirection(direction),
        showDepartures(direction.departuresMonFri),
        showDepartures(direction.departuresSat),
        showDepartures(direction.departuresSun)
      )).flatten()
    ).flatten()
  )),
  rotate(-90deg, reflow: true, [
    #strong(timetable.station) —
    #for (key, value) in directionMap {
      strong(key) + " " + value.join(", ") + " "
    }
  ]),
)
