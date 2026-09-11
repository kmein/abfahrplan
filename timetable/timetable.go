// Package timetable turns a GTFS feed into compact per-station departure
// timetables, in the shape timetable.typ renders to a PDF.
package timetable

import (
	"fmt"
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
	platforms map[string]int    // stop name -> platforms seen under it
	routes    map[string]string // route short name -> mode
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
	Kinds      []string `json:"kinds"` // one per entry in Routes
	Kind       string   `json:"kind"`  // what the station as a whole is
	Departures int      `json:"departures"`
}

// The modes a station can be, most distinctive first: a stop served by both the
// S-Bahn and a bus is an S-Bahn station to anyone looking for it.
var modeRank = []string{"sbahn", "ubahn", "rail", "tram", "ferry", "bus"}

// modeOf maps a GTFS route type to a mode. The VBB feed uses the extended
// route types, which is what makes tram and bus distinguishable at all -- M4
// is a tram and M19 a bus, and nothing in their names says so.
func modeOf(routeType int16) string {
	switch {
	case routeType == 109 || routeType == 2:
		return "sbahn"
	case routeType == 1 || (routeType >= 400 && routeType <= 405):
		return "ubahn"
	case routeType >= 100 && routeType < 200:
		return "rail"
	case routeType == 0 || routeType == 900:
		return "tram"
	case routeType == 4 || routeType == 1000 || routeType == 1200:
		return "ferry"
	default:
		return "bus"
	}
}

// Timetables is every station in the feed, built from one pass over it.
type Timetables struct {
	stations map[string]*station
	index    []Station
}

func containsCI(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// Bounds is a geographic box, in the order pmtiles and most tile tooling use.
type Bounds struct {
	MinLon, MinLat, MaxLon, MaxLat float64
}

// ParseBounds reads "minLon,minLat,maxLon,maxLat".
func ParseBounds(spec string) (*Bounds, error) {
	fields := strings.Split(spec, ",")
	if len(fields) != 4 {
		return nil, fmt.Errorf("want minLon,minLat,maxLon,maxLat, got %q", spec)
	}
	values := make([]float64, 4)
	for i, field := range fields {
		value, err := strconv.ParseFloat(strings.TrimSpace(field), 64)
		if err != nil {
			return nil, fmt.Errorf("bounds: %w", err)
		}
		values[i] = value
	}
	bounds := &Bounds{values[0], values[1], values[2], values[3]}
	if bounds.MinLon > bounds.MaxLon || bounds.MinLat > bounds.MaxLat {
		return nil, fmt.Errorf("bounds: min is greater than max in %q", spec)
	}
	return bounds, nil
}

func (b *Bounds) contains(lat, lon float64) bool {
	if b == nil {
		return true
	}
	return lat >= b.MinLat && lat <= b.MaxLat && lon >= b.MinLon && lon <= b.MaxLon
}

// Options narrow what a feed is asked for.
type Options struct {
	Routes []string // only these route short names
	Within *Bounds  // only stops inside this box
	// Trim is removed from station names and headsigns wherever it occurs. VBB
	// tags every Berlin stop "(Berlin)", which is noise in a timetable that is
	// only about Berlin -- and it is not always the last thing in the name:
	// "Hornstr.(Berlin)" omits the space, and 44 stops carry a platform
	// qualifier after it, as in "S+U Rathaus Steglitz (Berlin) [Schloßstr.]".
	Trim string
}

func (o Options) clean(name string) string {
	if o.Trim == "" {
		return name
	}
	token := strings.TrimSpace(o.Trim)
	cleaned := strings.ReplaceAll(name, " "+token, "")
	cleaned = strings.ReplaceAll(cleaned, token, "")
	return strings.TrimSpace(strings.ReplaceAll(cleaned, "  ", " "))
}

// collect walks every trip once, grouping departures into stations. stationOf
// names the station a stop belongs to, or returns false to skip the stop.
func (f *Feed) collect(stationOf func(*gtfs.Stop) (string, bool), opts Options) map[string]*station {
	stations := make(map[string]*station)
	departures := make(map[key]*departure)
	daysOf := make(map[*gtfs.Service]dayset)
	seenStop := make(map[*gtfs.Stop]bool)

	for _, trip := range f.feed.Trips {
		if len(opts.Routes) > 0 && !slices.Contains(opts.Routes, trip.Route.Short_name) {
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
			if !opts.Within.contains(float64(stop.Lat), float64(stop.Lon)) {
				continue
			}
			name, ok := stationOf(stop)
			if !ok {
				continue
			}

			current := stations[name]
			if current == nil {
				current = &station{
					byHour:    make(map[int8][]departure),
					platforms: make(map[string]int),
					routes:    make(map[string]string),
				}
				stations[name] = current
			}
			if !seenStop[stop] {
				seenStop[stop] = true
				current.platforms[opts.clean(stop.Name)]++
				if stop.Lat != 0 || stop.Lon != 0 {
					current.latSum += float64(stop.Lat)
					current.lonSum += float64(stop.Lon)
					current.coords++
				}
			}
			// A short name can carry more than one route type -- M1 is a tram and
			// also the bus that replaces it -- so keep the most distinctive mode
			// rather than whichever trip the map happened to yield last.
			mode := modeOf(trip.Route.Type)
			if known, seen := current.routes[trip.Route.Short_name]; !seen || slices.Index(modeRank, mode) < slices.Index(modeRank, known) {
				current.routes[trip.Route.Short_name] = mode
			}

			id := key{
				station:    name,
				hour:       stopTime.Departure_time().Hour,
				minute:     stopTime.Departure_time().Minute,
				direction:  trip.Direction_id,
				routeShort: trip.Route.Short_name,
			}
			headsign := opts.clean(headsignOf(&stopTime, trip))
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
func (f *Feed) Station(query string, opts Options) Day {
	const single = ""
	stations := f.collect(func(stop *gtfs.Stop) (string, bool) {
		return single, containsCI(stop.Name, query)
	}, opts)

	matched := stations[single]
	if matched == nil {
		return newDay(query, nil)
	}
	return newDay(title(matched.platforms, query), matched.byHour)
}

// All groups every stop in the feed by name. Timetables are built on demand
// rather than up front, so a caller writing them out one at a time never holds
// more than one marshalled timetable.
func (f *Feed) All(opts Options) *Timetables {
	stations := f.collect(func(stop *gtfs.Stop) (string, bool) {
		return opts.clean(stop.Name), true
	}, opts)

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

		kinds := make([]string, len(routes))
		kind := "bus"
		rank := len(modeRank)
		for i, route := range routes {
			kinds[i] = current.routes[route]
			if at := slices.Index(modeRank, kinds[i]); at >= 0 && at < rank {
				kind, rank = kinds[i], at
			}
		}

		entry := Station{Name: name, Slug: unique, Routes: routes, Kinds: kinds, Kind: kind, Departures: current.count}
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
