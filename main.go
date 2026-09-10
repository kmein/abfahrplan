package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/patrickbr/gtfsparser"
	"github.com/patrickbr/gtfsparser/gtfs"
	flag "github.com/spf13/pflag"
)

func ContainsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

type Departure struct {
	Minute     int8
	RouteShort string
	Headsign   string
	Weekdays   []string
	Direction  int8

	// the days this departure is actually served on, reduced to Weekdays
	// once every trip has been merged into it
	dates map[gtfs.Date]bool
}

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

var possibleWeekdays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// serviceDates returns the days a service is active on. The calendar.txt
// daymap alone does not tell us: services increasingly leave it empty and
// list their days in calendar_dates.txt instead, and even those that set it
// are split into fragments covering a few weeks each, so a single service
// says little about which weekdays a departure serves.
func serviceDates(service *gtfs.Service) []gtfs.Date {
	dates := []gtfs.Date{}
	end := service.GetLastDefinedDate()
	for date := service.GetFirstDefinedDate(); !date.GetTime().After(end.GetTime()); date = date.GetOffsettedDate(1) {
		if service.IsActiveOn(date) {
			dates = append(dates, date)
		}
	}
	return dates
}

// weekdayOccurrences counts how often each weekday falls within the period
// the feed is valid for.
func weekdayOccurrences(feed *gtfsparser.Feed) [7]int {
	var first, last gtfs.Date
	for _, service := range feed.Services {
		start, end := service.GetFirstDefinedDate(), service.GetLastDefinedDate()
		if !start.IsEmpty() && (first.IsEmpty() || start.GetTime().Before(first.GetTime())) {
			first = start
		}
		if !end.IsEmpty() && (last.IsEmpty() || end.GetTime().After(last.GetTime())) {
			last = end
		}
	}

	var occurrences [7]int
	for date := first; !first.IsEmpty() && !date.GetTime().After(last.GetTime()); date = date.GetOffsettedDate(1) {
		occurrences[int(date.GetTime().Weekday())]++
	}
	return occurrences
}

// regularWeekdays reduces the days a departure is served on to the weekdays it
// serves regularly. A departure running on a single Saturday out of seventeen
// is a one-off — an event or holiday extra — and has no place in a timetable
// of the ordinary week; one running every other Wednesday does.
func regularWeekdays(dates map[gtfs.Date]bool, occurrences [7]int) []string {
	var served [7]int
	for date := range dates {
		served[int(date.GetTime().Weekday())]++
	}

	weekdays := []string{}
	for i, day := range possibleWeekdays {
		if served[i] > 0 && 4*served[i] >= occurrences[i] {
			weekdays = append(weekdays, day)
		}
	}
	return weekdays
}

// stationTitle picks the name to head the timetable with. GTFS splits a station
// into one stop per platform, so take the name shared by the most of them
// rather than the search string that was typed. Counting platforms rather than
// departures keeps a busy neighbouring bus stop from lending its name to the
// station: at Hermannplatz the stop on Sonnenallee sees more departures than
// the U-Bahn does, but the U-Bahn is what the station is called.
func stationTitle(stopNames map[string]int, fallback string) string {
	title, most := fallback, 0
	for name, count := range stopNames {
		if count > most || (count == most && name < title) {
			title, most = name, count
		}
	}
	return title
}

func collectDepartures(feed *gtfsparser.Feed, routeNames []string, stationName string) (map[int8][]Departure, map[string]int) {
	departureTimes := make(map[int8][]Departure)
	stopNames := make(map[string]int)
	seenStops := make(map[*gtfs.Stop]bool)
	datesOf := make(map[*gtfs.Service][]gtfs.Date)
	for _, trip := range feed.Trips {
		for _, stopTime := range trip.StopTimes {
			stop := stopTime.Stop()
			if ContainsCI(stop.Name, stationName) {
				dates, cached := datesOf[trip.Service]
				if !cached {
					dates = serviceDates(trip.Service)
					datesOf[trip.Service] = dates
				}

				hour := stopTime.Departure_time().Hour
				minute := stopTime.Departure_time().Minute

				if len(routeNames) == 0 || slices.Contains(routeNames, trip.Route.Short_name) {
					if !seenStops[stop] {
						seenStops[stop] = true
						stopNames[stop.Name] += 1
					}
					// if any of the departureTimes[hour] have the same minute and routeshort and directionId, then instead of adding a new one, merge the days it is served on
					found := false
					for _, dep := range departureTimes[hour] {
						if dep.Minute == minute && dep.RouteShort == trip.Route.Short_name && dep.Direction == trip.Direction_id {
							for _, date := range dates {
								dep.dates[date] = true
							}
							found = true
							break
						}
					}
					if !found {
						served := make(map[gtfs.Date]bool, len(dates))
						for _, date := range dates {
							served[date] = true
						}
						departureTimes[hour] = append(departureTimes[hour], Departure{
							Minute:     minute,
							RouteShort: trip.Route.Short_name,
							Headsign:   *trip.Headsign,
							Direction:  trip.Direction_id,
							dates:      served,
						})
					}
				}
			}
		}
	}

	occurrences := weekdayOccurrences(feed)
	for hour := range departureTimes {
		for i := range departureTimes[hour] {
			departureTimes[hour][i].Weekdays = regularWeekdays(departureTimes[hour][i].dates, occurrences)
		}
	}
	return departureTimes, stopNames
}

type JsonDeparture struct {
	Minute           int8     `json:"minute"`
	Headsign         string   `json:"headsign"`
	ExcludedWeekdays []string `json:"excluded_weekdays,omitempty"`
}
type JsonDirection struct {
	DeparturesMonFri []JsonDeparture `json:"departuresMonFri"`
	DeparturesSat    []JsonDeparture `json:"departuresSat"`
	DeparturesSun    []JsonDeparture `json:"departuresSun"`
	Direction        int8            `json:"direction"`
	RouteShort       string          `json:"route_short"`
}
type JsonHour struct {
	Directions []JsonDirection `json:"directions"`
	Hour       int8            `json:"hour"`
}
type JsonDay struct {
	Hours   []JsonHour `json:"hours"`
	Station string     `json:"station"`
}

func jsonTimeTable(station string, departureTimes map[int8][]Departure) JsonDay {
	day := JsonDay{
		Hours:   make([]JsonHour, 0),
		Station: station,
	}
	for hour := int8(0); hour <= 30; hour++ {
		if departures, ok := departureTimes[hour]; ok {
			// group by route short name
			routeMap := make(map[string][]Departure)
			for _, dep := range departures {
				routeMap[dep.RouteShort] = append(routeMap[dep.RouteShort], dep)
			}

			jsonHour := JsonHour{
				Hour:       hour,
				Directions: make([]JsonDirection, 0),
			}

			// group by direction
			for routeShort, deps := range routeMap {

				directionMap := make(map[int8][]Departure)
				for _, dep := range deps {
					directionMap[dep.Direction] = append(directionMap[dep.Direction], dep)
				}
				for direction, ddeps := range directionMap {
					jsonDirection := JsonDirection{
						RouteShort:       routeShort,
						Direction:        direction,
						DeparturesMonFri: make([]JsonDeparture, 0),
						DeparturesSat:    make([]JsonDeparture, 0),
						DeparturesSun:    make([]JsonDeparture, 0),
					}

					// sort by minute
					sort.Slice(ddeps, func(i, j int) bool {
						return ddeps[i].Minute < ddeps[j].Minute
					})

					for _, dep := range ddeps {
						if intersects(dep.Weekdays, []string{"Mon", "Tue", "Wed", "Thu", "Fri"}) {
							jsonDirection.DeparturesMonFri = append(jsonDirection.DeparturesMonFri, JsonDeparture{
								Minute:           dep.Minute,
								Headsign:         dep.Headsign,
								ExcludedWeekdays: setDifference([]string{"Mon", "Tue", "Wed", "Thu", "Fri"}, dep.Weekdays),
							})
						}
						if isSuperset(dep.Weekdays, []string{"Sat"}) {
							jsonDirection.DeparturesSat = append(jsonDirection.DeparturesSat, JsonDeparture{
								Minute:   dep.Minute,
								Headsign: dep.Headsign,
							})
						}
						if isSuperset(dep.Weekdays, []string{"Sun"}) {
							jsonDirection.DeparturesSun = append(jsonDirection.DeparturesSun, JsonDeparture{
								Minute:   dep.Minute,
								Headsign: dep.Headsign,
							})
						}
					}
					jsonHour.Directions = append(jsonHour.Directions, jsonDirection)
				}
			}
			day.Hours = append(day.Hours, jsonHour)
		}
	}
	return day
}

func main() {

	// get stop name query from command line argument, use a library
	flag.Usage = func() {
		fmt.Printf("Usage: abfahrplan [options] <station name> <GTFS zip file>\n\n")
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
	}

	stationName := flag.StringP("station", "s", "", "Name of the station to search for")
	gtfsFile := flag.StringP("gtfs", "g", "GTFS.zip", "Path to the GTFS zip file")
	routeNames := flag.StringSliceP("route", "r", []string{}, "Filter by route short names (can be specified multiple times)")
	flag.Parse()

	fmt.Printf("Reading GTFS data from '%s'...\n", *gtfsFile)
	feed := gtfsparser.NewFeed()
	if err := feed.Parse(*gtfsFile); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading GTFS data: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("GTFS data read successfully.\n")

	fmt.Printf("Done, parsed %d agencies, %d stops, %d routes, %d trips, %d fare attributes\n\n", len(feed.Agencies), len(feed.Stops), len(feed.Routes), len(feed.Trips), len(feed.FareAttributes))

	departureTimes, stopNames := collectDepartures(feed, *routeNames, *stationName)

	allJson := jsonTimeTable(stationTitle(stopNames, *stationName), departureTimes)

	jsonData, err := json.MarshalIndent(allJson, "", "  ")
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return
	}
	err = os.WriteFile("timetable.json", jsonData, 0644)
	if err != nil {
		fmt.Println("Error writing JSON to file:", err)
		return
	}
	fmt.Println("Timetable written to timetable.json")
}
