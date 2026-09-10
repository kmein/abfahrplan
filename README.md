# abfahrplan

`abfahrplan` is a command-line tool for parsing and extracting departure information from GTFS (General Transit Feed Specification) data. It processes public transportation schedules and generates structured timetables for specific stations or stops.

## What is it?

`abfahrplan` (German for "departure schedule") reads GTFS data files and extracts departure times for a specified station. It organizes the data by:
- Hour of departure
- Route (line/service number)
- Direction
- Days of the week (Monday-Friday, Saturday, Sunday)

The tool outputs a JSON file that can be used to create compact, printable timetables or integrated into other applications. Together with the included Typst document that means one sheet of paper for the wall, styled after BVG's printed timetables.

## Features

- **GTFS Parser**: Reads standard GTFS zip files containing public transportation schedules
- **Station Search**: Finds departures for a specific station by name (case-insensitive partial matching)
- **Route Filtering**: Optionally filter results to show only specific routes/lines
- **Weekday Grouping**: Groups departures by weekday patterns (Mon-Fri, Sat, Sun) from the days each service actually runs, not just the `calendar.txt` bitmap
- **JSON Output**: Generates structured JSON data suitable for further processing
- **PDF Generation**: Can be combined with Typst to generate compact PDF timetables

## Requirements

- Go 1.24.5 or later
- A GTFS data file (zip format) from your public transportation provider
- Optional: [Typst](https://typst.app/) for PDF generation. The document is set in Fira Sans, a free stand-in for BVG's own Transit; the palette is sampled from BVG print PDFs

## Installation

```bash
go build
```

This will create the `abfahrplan` binary in the current directory.

## Usage

```bash
./abfahrplan [options]
```

### Options

- `-s, --station string` - Name of the station to search for (required)
- `-g, --gtfs string` - Path to the GTFS zip file (default: "GTFS.zip")
- `-r, --route strings` - Filter by route short names (can be specified multiple times)

### Examples

**Basic usage** - Extract all departures for a station:
```bash
./abfahrplan -s "Albrechtstr" -g GTFS.zip
```

**Filter by specific routes** - Show only specific bus/tram lines:
```bash
./abfahrplan -s "Albrechtstr" -r 140 -r M46
```

The station is matched as a substring, so `-s Albrechtstr` picks up every platform of `Albrechtstr./Manteuffelstr.` — and would equally catch an unrelated stop elsewhere whose name contains the same text. The timetable is headed with the name shared by the most matched platforms, not with the search string.

Expect around a minute per run: the whole feed is parsed to find one station.

## Output

The tool generates a `timetable.json` file containing:
- Station name
- Departures organized by hour (0-30, supporting overnight services)
- For each hour: route information, direction, and departures grouped by day type
- Excluded weekdays marked for irregular services

## Example Workflow

The included `Makefile` demonstrates a complete workflow:

1. Download GTFS data (e.g., from VBB Berlin)
2. Build the `abfahrplan` tool
3. Extract departure data for a specific station
4. Generate a compact PDF timetable using Typst

```bash
make all
```

This will:
- Download `GTFS.zip` from the VBB (Berlin/Brandenburg public transport)
- Build the `abfahrplan` binary
- Generate `timetable.json` for "Albrechtstr" station
- Compile `timetable.pdf` using Typst

### Keeping the feed current

The `GTFS.zip` rule has no freshness condition, so `make` will never replace a feed it has already downloaded. When a new timetable period starts:

```bash
rm GTFS.zip && make
```

## How the weekdays are worked out

A GTFS service says which days it runs on twice over: a weekday bitmap in `calendar.txt` and per-date exceptions in `calendar_dates.txt`. Reading only the bitmap is not enough — VBB increasingly leaves it empty and puts every day in `calendar_dates.txt`, and splits each service into fragments covering a few weeks each, so no single service describes a whole week.

So every departure accumulates the set of days it is actually served on, across all the trips that share its minute, line and direction. A weekday makes it into the timetable when the departure runs on at least a quarter of that weekday's occurrences in the feed period. That threshold keeps one-off event and holiday extras out of a timetable of the ordinary week, while keeping genuine every-other-week service in.

Departures that skip some weekdays are marked with the days they do run, in superscript.

## License

GNU Affero General Public License v3.0 - See LICENSE file for details.
