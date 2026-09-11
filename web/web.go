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

//go:embed app.css app.js vendor glyphs
var assets embed.FS

//go:embed index.html station.html footer.html legal.html
var pages embed.FS

//go:embed favicon.svg robots.txt
var rootAssets embed.FS

// Site is what every page needs to know about the build it belongs to.
type Site struct {
	ValidFrom      string
	ValidTo        string
	HasImpressum   bool
	HasDatenschutz bool
}

// page is one rendered page. Root is the relative path back to the site root,
// so the shared footer can link the same things from / and from /s/.
type page struct {
	Site
	Root  string
	Title string
	Body  template.HTML

	Slug  string
	Day   timetable.Day
	Kinds map[string]string // route short name -> mode
}

var templates = template.Must(template.New("abfahrplan").Funcs(template.FuncMap{
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
}).ParseFS(pages, "*.html"))

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

// Index writes the landing page: search box, map, footer.
func Index(out io.Writer, site Site) error {
	return templates.ExecuteTemplate(out, "index.html", page{Site: site, Root: ""})
}

// Station writes one station's page. kinds maps each route short name to its
// mode, so the badges match the colours on the map.
func Station(out io.Writer, slug string, day timetable.Day, site Site, kinds map[string]string) error {
	return templates.ExecuteTemplate(out, "station.html", page{
		Site: site, Root: "../", Slug: slug, Day: day, Kinds: kinds,
	})
}

// Legal writes an Impressum or Datenschutzerklaerung: the operator's own text,
// wrapped in the site's shell so it does not look like a different website.
func Legal(out io.Writer, site Site, title string, body []byte) error {
	return templates.ExecuteTemplate(out, "legal.html", page{
		Site: site, Root: "", Title: title, Body: template.HTML(body),
	})
}

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

// RootFiles are served from the root of the site rather than from static/,
// because that is where browsers and crawlers look for them.
func RootFiles() map[string][]byte {
	files := map[string][]byte{}
	for _, name := range []string{"favicon.svg", "robots.txt"} {
		if content, err := rootAssets.ReadFile(name); err == nil {
			files[name] = content
		}
	}
	return files
}

// Asset reads one embedded file, by its path in StaticFiles.
func Asset(path string) ([]byte, error) { return assets.ReadFile(path) }
