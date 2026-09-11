// Package web holds the static front end: the map, the search box and the
// per-station pages. Everything travels inside the binary, so a generated site
// has no external dependency at all -- no CDN, no tile server, no web fonts.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"strings"

	"github.com/kmein/abfahrplan/timetable"
)

//go:embed index.html app.css app.js vendor glyphs
var assets embed.FS

//go:embed station.html
var stationTemplate string

// Index is the landing page, written to the root of a build.
func Index() ([]byte, error) { return assets.ReadFile("index.html") }

// StaticFiles lists the paths under static/, mapped to their source in the
// embedded tree. vendor/ and glyphs/ are flattened one level so app.js can
// import "./maplibre-gl.mjs" beside itself.
func StaticFiles() (map[string]string, error) {
	files := map[string]string{"app.css": "app.css", "app.js": "app.js"}
	for _, dir := range []string{"vendor", "glyphs"} {
		err := fs.WalkDir(assets, dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || strings.HasSuffix(path, ".version") {
				return err
			}
			target := strings.TrimPrefix(path, dir+"/")
			if dir == "glyphs" {
				target = "glyphs/" + target
			}
			files[target] = path
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// Asset reads one embedded file, by its path in StaticFiles.
func Asset(path string) ([]byte, error) { return assets.ReadFile(path) }

// Meta is the part of a build's metadata the station pages show.
type Meta struct {
	ValidFrom string
	ValidTo   string
}

type stationPage struct {
	Slug  string
	Day   timetable.Day
	Meta  Meta
	Kinds map[string]string // route short name -> mode
}

var station = template.Must(template.New("station").Funcs(template.FuncMap{
	"badge": func(route string, kinds map[string]string) template.HTML {
		kind := kinds[route]
		if kind == "" {
			kind = "bus"
		}
		return template.HTML(fmt.Sprintf(`<span class="badge %s">%s</span>`, kind, template.HTMLEscapeString(route)))
	},
	"arrow": func(direction int8) string {
		if direction == 0 {
			return "→"
		}
		return "←"
	},
	"minutes": func(departures []timetable.Departure) template.HTML {
		parts := make([]string, 0, len(departures))
		for _, departure := range departures {
			minute := fmt.Sprintf("%02d", departure.Minute)
			if served := servedWeekdays(departure.ExcludedWeekdays); served != "" {
				minute += "<sup>" + served + "</sup>"
			}
			parts = append(parts, minute)
		}
		return template.HTML(strings.Join(parts, " "))
	},
}).Parse(stationTemplate))

// servedWeekdays turns the weekdays a departure is missing from into the ones
// it runs on, abbreviated as on a German timetable -- the same convention the
// PDF uses.
func servedWeekdays(excluded []string) string {
	if len(excluded) == 0 {
		return ""
	}
	short := map[string]string{"Mon": "Mo", "Tue": "Di", "Wed": "Mi", "Thu": "Do", "Fri": "Fr"}
	served := []string{}
	for _, day := range []string{"Mon", "Tue", "Wed", "Thu", "Fri"} {
		missing := false
		for _, gone := range excluded {
			if gone == day {
				missing = true
				break
			}
		}
		if !missing {
			served = append(served, short[day])
		}
	}
	return strings.Join(served, ",")
}

// Station writes one station's HTML page. kinds maps each route short name to
// its mode, so the badges match the colours on the map.
func Station(out io.Writer, slug string, day timetable.Day, meta Meta, kinds map[string]string) error {
	return station.Execute(out, stationPage{Slug: slug, Day: day, Meta: meta, Kinds: kinds})
}
