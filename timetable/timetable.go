// Package timetable turns a GTFS feed into compact per-station departure
// timetables, in the shape timetable.typ renders to a PDF.
package timetable

import (
	"iter"
	"slices"
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
				current = &station{byHour: make(map[int8][]departure), platforms: make(map[string]int)}
				stations[name] = current
			}
			if !seenStop[stop] {
				seenStop[stop] = true
				current.platforms[stop.Name]++
			}

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

// All returns a timetable for every station in the feed, keyed by stop name.
// The timetables are built as they are yielded, so a caller writing them out
// one at a time never holds more than one.
func (f *Feed) All(routeNames ...string) iter.Seq2[string, Day] {
	stations := f.collect(func(stop *gtfs.Stop) (string, bool) {
		return stop.Name, true
	}, routeNames)

	return func(yield func(string, Day) bool) {
		for name, current := range stations {
			if !yield(name, newDay(name, current.byHour)) {
				return
			}
		}
	}
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
