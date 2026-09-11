// Package timetable turns a GTFS feed into compact per-station departure
// timetables, in the shape timetable.typ renders to a PDF.
package timetable

import (
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrickbr/gtfsparser"
	"github.com/patrickbr/gtfsparser/gtfs"
)

// Feed is a parsed GTFS feed, ready to be asked for timetables.
type Feed struct {
	feed     *gtfsparser.Feed
	calendar *calendar
}

// Load parses the GTFS zip at path.
func Load(path string) (*Feed, error) {
	feed := gtfsparser.NewFeed()
	// shapes.txt is 182 MB of the VBB feed's 670 MB uncompressed and nothing
	// here has ever touched a shape.
	feed.SetParseOpts(gtfsparser.ParseOptions{DropShapes: true})
	if err := feed.Parse(path); err != nil {
		return nil, err
	}
	return &Feed{feed: feed, calendar: newCalendar(feed)}, nil
}

// Counts reports what was parsed, for the CLI's progress line.
func (f *Feed) Counts() (agencies, stops, routes, trips, fares int) {
	return len(f.feed.Agencies), len(f.feed.Stops), len(f.feed.Routes), len(f.feed.Trips), len(f.feed.FareAttributes)
}

// Validity returns the period the feed covers.
func (f *Feed) Validity() (from, to time.Time) {
	if f.calendar.days == 0 {
		return time.Time{}, time.Time{}
	}
	return f.calendar.origin.GetTime(), f.calendar.origin.GetOffsettedDate(f.calendar.days - 1).GetTime()
}

// departure is one row of a timetable while it is being built: the days it runs
// on are still a bitset, not yet reduced to weekday names.
type departure struct {
	Minute     int8
	RouteShort string
	Headsign   string
	Direction  int8
	Weekdays   []string

	days dayset
	// how many days the service that supplied Headsign runs on, so a merge
	// can keep the most representative one instead of whichever trip the map
	// happened to yield first
	headsignDays int
}

// headsignOf is where a trip is going, as shown on the vehicle. trip_headsign
// is optional in GTFS and stop_times.txt may override it per stop, so take the
// most specific one actually present rather than dereferencing blindly.
func headsignOf(stopTime *gtfs.StopTime, trip *gtfs.Trip) string {
	if headsign := stopTime.Headsign(); headsign != nil && *headsign != "" {
		return *headsign
	}
	if trip.Headsign != nil && *trip.Headsign != "" {
		return *trip.Headsign
	}
	if trip.Route == nil {
		return ""
	}
	if trip.Route.Long_name != "" {
		return trip.Route.Long_name
	}
	return trip.Route.Short_name
}

// departures sharing a key are the same departure seen from different trips,
// and are merged into one row.
type key struct {
	station    string
	hour       int8
	minute     int8
	direction  int8
	routeShort string
}

type station struct {
	byHour    map[int8][]departure
	platforms map[string]int // stop name -> platforms seen under it
	routes    map[string]bool
	latSum    float64
	lonSum    float64
	coords    int
	count     int
}

// Station is everything sharing a stop name: one entry for the picker, at the
// centroid of its platforms.
type Station struct {
	Name       string   `json:"name"`
	Slug       string   `json:"slug"`
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	Routes     []string `json:"routes"`
	Departures int      `json:"departures"`
}

// Timetables is every station in the feed, built from one pass over it.
type Timetables struct {
	stations map[string]*station
	index    []Station
}

func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// collect walks every trip once, grouping departures into stations. stationOf
// names the station a stop belongs to, or returns false to skip the stop.
func (f *Feed) collect(stationOf func(*gtfs.Stop) (string, bool), routeNames []string) map[string]*station {
	stations := make(map[string]*station)
	departures := make(map[key]*departure)
	daysOf := make(map[*gtfs.Service]dayset)
	seenStop := make(map[*gtfs.Stop]bool)

	for _, trip := range f.feed.Trips {
		if len(routeNames) > 0 && !slices.Contains(routeNames, trip.Route.Short_name) {
			continue
		}

		days, cached := daysOf[trip.Service]
		if !cached {
			days = f.calendar.serviceDays(trip.Service)
			daysOf[trip.Service] = days
		}
		dayCount := days.count()

		for i, stopTime := range trip.StopTimes {
			// the last stop is where the trip terminates: an arrival, not a
			// departure anyone can board
			if i == len(trip.StopTimes)-1 {
				continue
			}
			// pickup_type 1 is "no pickup available" -- the vehicle passes
			// through or only lets passengers off
			if stopTime.Pickup_type() == 1 {
				continue
			}

			stop := stopTime.Stop()
			name, ok := stationOf(stop)
			if !ok {
				continue
			}

			current := stations[name]
			if current == nil {
				current = &station{
					byHour:    make(map[int8][]departure),
					platforms: make(map[string]int),
					routes:    make(map[string]bool),
				}
				stations[name] = current
			}
			if !seenStop[stop] {
				seenStop[stop] = true
				current.platforms[stop.Name]++
				if stop.Lat != 0 || stop.Lon != 0 {
					current.latSum += float64(stop.Lat)
					current.lonSum += float64(stop.Lon)
					current.coords++
				}
			}
			current.routes[trip.Route.Short_name] = true

			id := key{
				station:    name,
				hour:       stopTime.Departure_time().Hour,
				minute:     stopTime.Departure_time().Minute,
				direction:  trip.Direction_id,
				routeShort: trip.Route.Short_name,
			}
			headsign := headsignOf(&stopTime, trip)
			if existing := departures[id]; existing != nil {
				existing.days.or(days)
				// keep the headsign of the service that runs most often, ties
				// broken lexicographically, so consecutive runs agree
				if dayCount > existing.headsignDays || (dayCount == existing.headsignDays && headsign < existing.Headsign) {
					existing.Headsign, existing.headsignDays = headsign, dayCount
				}
				continue
			}
			departures[id] = &departure{
				Minute:       id.minute,
				RouteShort:   id.routeShort,
				Headsign:     headsign,
				Direction:    id.direction,
				days:         slices.Clone(days),
				headsignDays: dayCount,
			}
		}
	}

	for id, departure := range departures {
		departure.Weekdays = f.calendar.regularWeekdays(departure.days)
		current := stations[id.station]
		current.byHour[id.hour] = append(current.byHour[id.hour], *departure)
		current.count++
	}
	return stations
}

// Station returns the timetable for the stops whose name contains query,
// merged into one. Optionally restricted to the given route short names.
func (f *Feed) Station(query string, routeNames ...string) Day {
	const single = ""
	stations := f.collect(func(stop *gtfs.Stop) (string, bool) {
		return single, containsCI(stop.Name, query)
	}, routeNames)

	matched := stations[single]
	if matched == nil {
		return newDay(query, nil)
	}
	return newDay(title(matched.platforms, query), matched.byHour)
}

// All groups every stop in the feed by name. Timetables are built on demand
// rather than up front, so a caller writing them out one at a time never holds
// more than one marshalled timetable.
func (f *Feed) All(routeNames ...string) *Timetables {
	stations := f.collect(func(stop *gtfs.Stop) (string, bool) {
		return stop.Name, true
	}, routeNames)

	names := make([]string, 0, len(stations))
	for name := range stations {
		names = append(names, name)
	}
	sort.Strings(names)

	// slugs are assigned in name order, so a collision always resolves the same
	// way from one build to the next
	taken := make(map[string]bool, len(names))
	index := make([]Station, 0, len(names))
	for _, name := range names {
		current := stations[name]
		slug := Slug(name)
		if slug == "" {
			slug = "station"
		}
		unique := slug
		for n := 2; taken[unique]; n++ {
			unique = slug + "-" + strconv.Itoa(n)
		}
		taken[unique] = true

		routes := make([]string, 0, len(current.routes))
		for route := range current.routes {
			routes = append(routes, route)
		}
		sort.Strings(routes)

		entry := Station{Name: name, Slug: unique, Routes: routes, Departures: current.count}
		if current.coords > 0 {
			// five decimals is about a metre; the raw float32s carry conversion
			// noise that would only bloat the index every visitor downloads
			entry.Lat = math.Round(current.latSum/float64(current.coords)*1e5) / 1e5
			entry.Lon = math.Round(current.lonSum/float64(current.coords)*1e5) / 1e5
		}
		index = append(index, entry)
	}
	return &Timetables{stations: stations, index: index}
}

// Stations lists every station, ordered by name, with its slug assigned.
func (t *Timetables) Stations() []Station { return t.index }

// Day builds one station's timetable.
func (t *Timetables) Day(name string) Day {
	current := t.stations[name]
	if current == nil {
		return newDay(name, nil)
	}
	return newDay(name, current.byHour)
}

// title picks the name to head the timetable with. GTFS splits a station into
// one stop per platform, so take the name shared by the most of them rather
// than the search string that was typed. Counting platforms rather than
// departures keeps a busy neighbouring bus stop from lending its name to the
// station: at Hermannplatz the stop on Sonnenallee sees more departures than
// the U-Bahn does, but the U-Bahn is what the station is called.
func title(platforms map[string]int, fallback string) string {
	name, most := fallback, 0
	for candidate, count := range platforms {
		if count > most || (count == most && candidate < name) {
			name, most = candidate, count
		}
	}
	return name
}
