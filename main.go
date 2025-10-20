package main

import (
	"fmt"
	"github.com/patrickbr/gtfsparser"
	flag "github.com/spf13/pflag"
	"strings"
)

func ContainsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// custom type for departure
type Departure struct {
	Minute     int8
	RouteShort string
	Headsign   string
	Weekdays   []string
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

	// find all trips, filter trip.stopTimes by stop name

	// collect departures by hour, sorted by minute (also route.short_name and trip.headsign)
	departureTimes := make(map[int8][]Departure)
	for _, trip := range feed.Trips {
		for _, stopTime := range trip.StopTimes {
			stop := stopTime.Stop()
			if ContainsCI(stop.Name, *stationName) {
				possibleWeekdays := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
				weekdays := []string{}

				for i, day := range possibleWeekdays {
					if trip.Service.Daymap(i + 1) {
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
				})
			}
		}
	}

	/*
	display in the following format
	03
		246
			S+U Alexanderplatz  1 (Mon Tue) 15 (Mon Tue Wed Thu Fri Sat Sun)
			S+U Friedrichstr.   00 (Mon Tue Wed Thu Fri Sat Sun) 45 (Mon Tue Wed Thu Fri Sat Sun) 

	*/

	// print departures by hour (sorted by minute)
	for hour := int8(0); hour <= 24; hour++ {
		if departures, ok := departureTimes[hour]; ok {
			fmt.Printf("%02d: ", hour)
			// sort departures by minute
			for i := 0; i < len(departures)-1; i++ {
				for j := i + 1; j < len(departures); j++ {
					if departures[i].Minute > departures[j].Minute {
						departures[i], departures[j] = departures[j], departures[i]
					}
				}
			}
			for _, departure := range departures {
				fmt.Printf("%02d (%s %s %s) ", departure.Minute, departure.RouteShort, departure.Headsign, departure.Weekdays)
			}
			fmt.Printf("\n")
		}
	}

}
