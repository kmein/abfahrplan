package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/patrickbr/gtfsparser"
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

func collectDepartures(feed *gtfsparser.Feed, stationName string) map[int8][]Departure {
	departureTimes := make(map[int8][]Departure)
	for _, trip := range feed.Trips {
		for _, stopTime := range trip.StopTimes {
			stop := stopTime.Stop()
			if ContainsCI(stop.Name, stationName) {
				possibleWeekdays := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
				weekdays := []string{}

				for i, day := range possibleWeekdays {
					if trip.Service.Daymap(i) {
						weekdays = append(weekdays, day)
					}
				}

				hour := stopTime.Departure_time().Hour
				minute := stopTime.Departure_time().Minute
				departureTimes[hour] = append(departureTimes[hour], Departure{
					Minute:     minute,
					RouteShort: trip.Route.Short_name,
					Headsign:   *trip.Headsign,
					Weekdays:   weekdays,
					Direction:  trip.Direction_id,
				})
			}
		}
	}
	return departureTimes
}

type JsonDeparture struct {
	Minute   int8   `json:"minute"`
	Headsign string `json:"headsign"`
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
	Hours []JsonHour `json:"hours"`
  Station string     `json:"station"`
}

func jsonTimeTable(station string, departureTimes map[int8][]Departure) JsonDay {
	day := JsonDay{
		Hours: make([]JsonHour, 0),
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
						if isSuperset(dep.Weekdays, []string{"Mon", "Tue", "Wed", "Thu", "Fri"}) {
							jsonDirection.DeparturesMonFri = append(jsonDirection.DeparturesMonFri, JsonDeparture{
								Minute:   dep.Minute,
								Headsign: dep.Headsign,
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
	flag.Parse()

	fmt.Printf("Reading GTFS data from '%s'...\n", *gtfsFile)
	feed := gtfsparser.NewFeed()
	feed.Parse(*gtfsFile)
	fmt.Printf("GTFS data read successfully.\n")

	fmt.Printf("Done, parsed %d agencies, %d stops, %d routes, %d trips, %d fare attributes\n\n", len(feed.Agencies), len(feed.Stops), len(feed.Routes), len(feed.Trips), len(feed.FareAttributes))

	departureTimes := collectDepartures(feed, *stationName)

	allJson := jsonTimeTable(*stationName, departureTimes)

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
