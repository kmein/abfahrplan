package timetable

import (
	"math/bits"
	"time"

	"github.com/patrickbr/gtfsparser"
	"github.com/patrickbr/gtfsparser/gtfs"
)

var weekdayNames = [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// dayset is a bitset over the days of a feed's validity period, one bit per day
// since the calendar's origin.
//
// The days a departure is served on used to be a map[gtfs.Date]bool. That is
// fine for one station, but a whole feed holds three million departures at a
// hundred-odd days each, which is several gigabytes of hashmap. The same
// information fits in two words.
type dayset []uint64

func (d dayset) set(day int) { d[day/64] |= 1 << uint(day%64) }

func (d dayset) or(other dayset) {
	for i := range d {
		d[i] |= other[i]
	}
}

// countMasked counts the days set in both d and mask.
func (d dayset) countMasked(mask dayset) int {
	n := 0
	for i := range d {
		n += bits.OnesCount64(d[i] & mask[i])
	}
	return n
}

func (d dayset) empty() bool {
	for _, word := range d {
		if word != 0 {
			return false
		}
	}
	return true
}

// calendar is the period a feed is valid for, and the machinery to talk about
// the days in it by index.
type calendar struct {
	origin      gtfs.Date
	days        int
	words       int
	weekdayMask [7]dayset // the days of the period falling on each weekday
	occurrences [7]int    // how often each weekday falls in the period
}

func newCalendar(feed *gtfsparser.Feed) *calendar {
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

	calendar := &calendar{origin: first, words: 1}
	if first.IsEmpty() || last.IsEmpty() {
		return calendar
	}

	calendar.days = int(last.GetTime().Sub(first.GetTime())/(24*time.Hour)) + 1
	calendar.words = (calendar.days + 63) / 64
	for i := range calendar.weekdayMask {
		calendar.weekdayMask[i] = make(dayset, calendar.words)
	}
	for day, date := 0, first; day < calendar.days; day, date = day+1, date.GetOffsettedDate(1) {
		weekday := int(date.GetTime().Weekday())
		calendar.weekdayMask[weekday].set(day)
		calendar.occurrences[weekday]++
	}
	return calendar
}

func (c *calendar) newDayset() dayset { return make(dayset, c.words) }

// index returns the day number of a date within the period, or -1 if it falls
// outside it.
func (c *calendar) index(date gtfs.Date) int {
	if c.days == 0 {
		return -1
	}
	day := int(date.GetTime().Sub(c.origin.GetTime()) / (24 * time.Hour))
	if day < 0 || day >= c.days {
		return -1
	}
	return day
}

// serviceDays returns the days a service is active on. The calendar.txt daymap
// does not tell us on its own: services increasingly leave it empty and list
// their days in calendar_dates.txt instead, and even those that set it are
// split into fragments covering a few weeks each, so a single service says
// little about which weekdays a departure serves.
func (c *calendar) serviceDays(service *gtfs.Service) dayset {
	days := c.newDayset()
	end := service.GetLastDefinedDate()
	for date := service.GetFirstDefinedDate(); !date.GetTime().After(end.GetTime()); date = date.GetOffsettedDate(1) {
		if !service.IsActiveOn(date) {
			continue
		}
		if day := c.index(date); day >= 0 {
			days.set(day)
		}
	}
	return days
}

// regularWeekdays reduces the days a departure is served on to the weekdays it
// serves regularly. A departure running on a single Saturday out of seventeen
// is a one-off — an event or holiday extra — and has no place in a timetable of
// the ordinary week; one running every other Wednesday does.
func (c *calendar) regularWeekdays(days dayset) []string {
	weekdays := []string{}
	for i, name := range weekdayNames {
		served := days.countMasked(c.weekdayMask[i])
		if served > 0 && 4*served >= c.occurrences[i] {
			weekdays = append(weekdays, name)
		}
	}
	return weekdays
}
