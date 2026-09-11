package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/kmein/abfahrplan/timetable"
	flag "github.com/spf13/pflag"
)

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
	feed, err := timetable.Load(*gtfsFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading GTFS data: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("GTFS data read successfully.\n")

	agencies, stops, routes, trips, fareAttributes := feed.Counts()
	fmt.Printf("Done, parsed %d agencies, %d stops, %d routes, %d trips, %d fare attributes\n\n", agencies, stops, routes, trips, fareAttributes)

	jsonData, err := json.MarshalIndent(feed.Station(*stationName, *routeNames...), "", "  ")
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
