package timetable

import "sort"

// The JSON below is the contract timetable.typ reads. Its shape is fixed.

type Departure struct {
	Minute           int8     `json:"minute"`
	Headsign         string   `json:"headsign"`
	ExcludedWeekdays []string `json:"excluded_weekdays,omitempty"`
}

type Direction struct {
	DeparturesMonFri []Departure `json:"departuresMonFri"`
	DeparturesSat    []Departure `json:"departuresSat"`
	DeparturesSun    []Departure `json:"departuresSun"`
	Direction        int8        `json:"direction"`
	RouteShort       string      `json:"route_short"`
	// Kind is the line's mode, so the sheet can colour a tram as a tram. The
	// name cannot be trusted for this: M4 is a tram and M19 a bus.
	Kind string `json:"kind"`
}

type Hour struct {
	Directions []Direction `json:"directions"`
	Hour       int8        `json:"hour"`
}

// Day is one station's timetable, ready to be marshalled for timetable.typ.
type Day struct {
	Hours   []Hour `json:"hours"`
	Station string `json:"station"`
}

var workweek = []string{"Mon", "Tue", "Wed", "Thu", "Fri"}

func intersects(set1, set2 []string) bool {
	setMap := make(map[string]bool)
	for _, s := range set1 {
		setMap[s] = true
	}
	for _, s := range set2 {
		if setMap[s] {
			return true
		}
	}
	return false
}

func setDifference(set1, set2 []string) []string {
	setMap := make(map[string]bool)
	for _, s := range set2 {
		setMap[s] = true
	}
	diff := []string{}
	for _, s := range set1 {
		if !setMap[s] {
			diff = append(diff, s)
		}
	}
	return diff
}

func isSuperset(set, subset []string) bool {
	setMap := make(map[string]bool)
	for _, s := range set {
		setMap[s] = true
	}
	for _, s := range subset {
		if !setMap[s] {
			return false
		}
	}
	return true
}

func newDay(station string, departureTimes map[int8][]departure, modes map[string]string) Day {
	day := Day{
		Hours:   make([]Hour, 0),
		Station: station,
	}
	for hour := int8(0); hour <= 30; hour++ {
		departures, ok := departureTimes[hour]
		if !ok {
			continue
		}

		// group by route short name
		routeMap := make(map[string][]departure)
		for _, dep := range departures {
			routeMap[dep.RouteShort] = append(routeMap[dep.RouteShort], dep)
		}

		jsonHour := Hour{
			Hour:       hour,
			Directions: make([]Direction, 0),
		}

		// group by direction
		for routeShort, deps := range routeMap {
			directionMap := make(map[int8][]departure)
			for _, dep := range deps {
				directionMap[dep.Direction] = append(directionMap[dep.Direction], dep)
			}
			for direction, ddeps := range directionMap {
				kind := modes[routeShort]
				if kind == "" {
					kind = "bus"
				}
				jsonDirection := Direction{
					RouteShort:       routeShort,
					Kind:             kind,
					Direction:        direction,
					DeparturesMonFri: make([]Departure, 0),
					DeparturesSat:    make([]Departure, 0),
					DeparturesSun:    make([]Departure, 0),
				}

				// sort by minute
				sort.Slice(ddeps, func(i, j int) bool {
					return ddeps[i].Minute < ddeps[j].Minute
				})

				for _, dep := range ddeps {
					if intersects(dep.Weekdays, workweek) {
						jsonDirection.DeparturesMonFri = append(jsonDirection.DeparturesMonFri, Departure{
							Minute:           dep.Minute,
							Headsign:         dep.Headsign,
							ExcludedWeekdays: setDifference(workweek, dep.Weekdays),
						})
					}
					if isSuperset(dep.Weekdays, []string{"Sat"}) {
						jsonDirection.DeparturesSat = append(jsonDirection.DeparturesSat, Departure{
							Minute:   dep.Minute,
							Headsign: dep.Headsign,
						})
					}
					if isSuperset(dep.Weekdays, []string{"Sun"}) {
						jsonDirection.DeparturesSun = append(jsonDirection.DeparturesSun, Departure{
							Minute:   dep.Minute,
							Headsign: dep.Headsign,
						})
					}
				}
				jsonHour.Directions = append(jsonHour.Directions, jsonDirection)
			}
		}
		// grouping above walks maps, so order the result: two runs over the
		// same feed should produce the same bytes
		sort.Slice(jsonHour.Directions, func(i, j int) bool {
			if jsonHour.Directions[i].RouteShort != jsonHour.Directions[j].RouteShort {
				return jsonHour.Directions[i].RouteShort < jsonHour.Directions[j].RouteShort
			}
			return jsonHour.Directions[i].Direction < jsonHour.Directions[j].Direction
		})
		day.Hours = append(day.Hours, jsonHour)
	}
	return day
}
