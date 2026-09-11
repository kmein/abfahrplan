// Command abfahrplan-generate turns a GTFS feed into a directory of static
// files: one JSON timetable and one PDF per station, plus an index for the
// search box and the map. Point a web server at it; there is nothing to run.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kmein/abfahrplan/render"
	"github.com/kmein/abfahrplan/timetable"
	"github.com/kmein/abfahrplan/web"
	flag "github.com/spf13/pflag"
)

type meta struct {
	Version    string    `json:"version"`
	Bounds     []float64 `json:"bounds,omitempty"`
	ValidFrom  string    `json:"valid_from"`
	ValidTo    string    `json:"valid_to"`
	BuiltAt    time.Time `json:"built_at"`
	Stations   int       `json:"stations"`
	Departures int       `json:"departures"`
}

func main() {
	gtfsFile := flag.StringP("gtfs", "g", "GTFS.zip", "Path to the GTFS zip file")
	feedURL := flag.String("url", "", "Download the feed from here into --gtfs first, skipping the build if it has not changed")
	outDir := flag.StringP("out", "o", "site", "Directory to generate into")
	jobs := flag.IntP("jobs", "j", runtime.NumCPU(), "How many timetables to render at once")
	keep := flag.Int("keep", 2, "How many previous builds to keep")
	force := flag.Bool("force", false, "Publish even if the feed's validity period has ended")
	limit := flag.Int("limit", 0, "Only generate this many stations (0 = all), for smoke tests")
	bbox := flag.String("bbox", "", "Only stations inside minLon,minLat,maxLon,maxLat")
	basemap := flag.String("basemap", "", "PMTiles basemap to publish alongside the site")
	trim := flag.String("trim", "(Berlin)", "Remove this text from station names and headsigns")
	flag.Parse()

	var bounds *timetable.Bounds
	if *bbox != "" {
		parsed, err := timetable.ParseBounds(*bbox)
		if err != nil {
			fmt.Fprintf(os.Stderr, "abfahrplan-generate: %v\n", err)
			os.Exit(1)
		}
		bounds = parsed
	}

	if err := generate(*gtfsFile, *feedURL, *outDir, *basemap, *trim, bounds, *jobs, *keep, *limit, *force); err != nil {
		fmt.Fprintf(os.Stderr, "abfahrplan-generate: %v\n", err)
		os.Exit(1)
	}
}

func generate(gtfsFile, feedURL, outDir, basemap, trim string, bounds *timetable.Bounds, jobs, keep, limit int, force bool) error {
	if feedURL != "" {
		changed, err := fetchFeed(feedURL, gtfsFile)
		if err != nil {
			return err
		}
		// Nothing new to publish, and rebuilding would only burn ten minutes
		// of CPU to produce the same tree.
		if !changed && !force {
			if _, err := os.Stat(filepath.Join(outDir, "current")); err == nil {
				log("nothing to do")
				return nil
			}
		}
	}

	version, err := fingerprint(gtfsFile)
	if err != nil {
		return err
	}

	started := time.Now()
	log("reading %s", gtfsFile)
	feed, err := timetable.Load(gtfsFile)
	if err != nil {
		return fmt.Errorf("reading GTFS data: %w", err)
	}

	from, to := feed.Validity()
	if to.IsZero() {
		return fmt.Errorf("feed declares no validity period")
	}
	// A timetable nobody can travel by is worse than yesterday's: refuse it and
	// leave whatever is published in place.
	if to.Before(time.Now()) && !force {
		return fmt.Errorf("feed expired on %s; refusing to publish it (use --force to override)", to.Format("2006-01-02"))
	}

	log("collecting departures for every station")
	all := feed.All(timetable.Options{Within: bounds, Trim: trim})
	stations := all.Stations()
	if limit > 0 && limit < len(stations) {
		stations = stations[:limit]
	}

	departures := 0
	for _, station := range stations {
		departures += station.Departures
	}
	log("%d stations, %d departures, feed valid %s to %s (parsed in %s)",
		len(stations), departures, from.Format("2006-01-02"), to.Format("2006-01-02"), time.Since(started).Round(time.Second))

	buildDir := filepath.Join(outDir, "builds", version)
	if _, err := os.Stat(buildDir); err == nil {
		// Rebuilding a feed we already have. Publish beside the old build and
		// let pruning drop it afterwards: deleting it here would take out the
		// directory `current` still points at, and with it the live site.
		buildDir = fmt.Sprintf("%s-%d", buildDir, time.Now().Unix())
	}
	workDir := buildDir + ".tmp"
	if err := os.RemoveAll(workDir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(workDir, "s"), 0o755); err != nil {
		return err
	}

	if err := writeJSON(filepath.Join(workDir, "stations.json"), stations); err != nil {
		return err
	}
	published := meta{
		Version:    version,
		ValidFrom:  from.Format("2006-01-02"),
		ValidTo:    to.Format("2006-01-02"),
		BuiltAt:    time.Now().UTC().Truncate(time.Second),
		Stations:   len(stations),
		Departures: departures,
	}
	if bounds != nil {
		published.Bounds = []float64{bounds.MinLon, bounds.MinLat, bounds.MaxLon, bounds.MaxLat}
	}
	if err := writeJSON(filepath.Join(workDir, "meta.json"), published); err != nil {
		return err
	}

	if basemap != "" {
		if err := copyFile(basemap, filepath.Join(workDir, "basemap.pmtiles")); err != nil {
			return fmt.Errorf("publishing basemap: %w", err)
		}
		log("published basemap from %s", basemap)
	}

	if err := writeFrontEnd(workDir); err != nil {
		return fmt.Errorf("writing the front end: %w", err)
	}

	pages := web.Meta{ValidFrom: from.Format("2006-01-02"), ValidTo: to.Format("2006-01-02")}
	if err := renderAll(all, stations, workDir, pages, jobs); err != nil {
		return err
	}

	if err := os.Rename(workDir, buildDir); err != nil {
		return err
	}
	if err := publish(outDir, filepath.Base(buildDir)); err != nil {
		return err
	}
	if err := prune(outDir, filepath.Base(buildDir), keep); err != nil {
		return err
	}

	log("published build %s in %s", filepath.Base(buildDir), time.Since(started).Round(time.Second))
	return nil
}

// renderAll writes every station's JSON and PDF, rendering in parallel because
// Typst is the whole cost of a build.
// writeFrontEnd unpacks the embedded page, its stylesheet and script, and the
// vendored MapLibre and PMTiles libraries with the map's glyph ranges. Nothing
// here is fetched at runtime, so the published tree has no CDN to outlive it.
func writeFrontEnd(workDir string) error {
	index, err := web.Index()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(workDir, "index.html"), index, 0o644); err != nil {
		return err
	}

	files, err := web.StaticFiles()
	if err != nil {
		return err
	}
	for target, source := range files {
		content, err := web.Asset(source)
		if err != nil {
			return err
		}
		path := filepath.Join(workDir, "static", target)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			return err
		}
	}
	log("wrote the front end (%d static files)", len(files)+1)
	return nil
}

func renderAll(all *timetable.Timetables, stations []timetable.Station, workDir string, pages web.Meta, jobs int) error {
	if jobs < 1 {
		jobs = 1
	}
	queue := make(chan timetable.Station)
	var done atomic.Int64
	var once sync.Once
	var failure error

	var workers sync.WaitGroup
	for range jobs {
		workers.Add(1)
		go func() {
			defer workers.Done()
			renderer := new(render.Renderer)
			for station := range queue {
				day := all.Day(station.Name)
				base := filepath.Join(workDir, "s", station.Slug)
				if err := writeJSON(base+".json", day); err != nil {
					once.Do(func() { failure = err })
					return
				}
				pdf, err := renderer.PDF(context.Background(), day)
				if err != nil {
					once.Do(func() { failure = fmt.Errorf("rendering %s: %w", station.Name, err) })
					return
				}
				if err := os.WriteFile(base+".pdf", pdf, 0o644); err != nil {
					once.Do(func() { failure = err })
					return
				}
				page, err := os.Create(base + ".html")
				if err != nil {
					once.Do(func() { failure = err })
					return
				}
				kinds := make(map[string]string, len(station.Routes))
				for i, route := range station.Routes {
					if i < len(station.Kinds) {
						kinds[route] = station.Kinds[i]
					}
				}
				if err := web.Station(page, station.Slug, day, pages, kinds); err != nil {
					page.Close()
					once.Do(func() { failure = fmt.Errorf("page for %s: %w", station.Name, err) })
					return
				}
				if err := page.Close(); err != nil {
					once.Do(func() { failure = err })
					return
				}
				if n := done.Add(1); n%500 == 0 {
					log("rendered %d/%d", n, len(stations))
				}
			}
		}()
	}

	for _, station := range stations {
		queue <- station
	}
	close(queue)
	workers.Wait()
	return failure
}

// publish points <out>/current at the new build. rename(2) over a symlink is
// atomic, so a web server serving out of it never sees a half-built tree.
func publish(outDir, version string) error {
	link := filepath.Join(outDir, "current")
	staging := link + ".tmp"
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.Symlink(filepath.Join("builds", version), staging); err != nil {
		return err
	}
	return os.Rename(staging, link)
}

func prune(outDir, current string, keep int) error {
	entries, err := os.ReadDir(filepath.Join(outDir, "builds"))
	if err != nil {
		return err
	}
	type build struct {
		name string
		at   time.Time
	}
	builds := []build{}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		builds = append(builds, build{entry.Name(), info.ModTime()})
	}
	sort.Slice(builds, func(i, j int) bool { return builds[i].at.After(builds[j].at) })

	for i, old := range builds {
		if i < keep-1 {
			continue
		}
		if err := os.RemoveAll(filepath.Join(outDir, "builds", old.name)); err != nil {
			return err
		}
		log("pruned old build %s", old.name)
	}
	return nil
}

// fingerprint names a build after the feed it came from, so republishing an
// unchanged feed is a no-op and two builds of the same feed collide on purpose.
func fingerprint(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil))[:12], nil
}

func copyFile(from, to string) error {
	source, err := os.Open(from)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(to)
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Close()
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func log(format string, args ...any) {
	fmt.Printf("%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}
