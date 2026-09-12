package web

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/kmein/abfahrplan/timetable"
)

// An import map address must start with /, ./ or ../. A bare "static/app.mjs"
// is dropped as invalid, silently: the page loads, nothing logs a warning, and
// every bare specifier on it fails to resolve at the moment someone clicks.
// That shipped once, on the index page only, where it broke the PDF button.
func TestImportMapAddressesResolve(t *testing.T) {
	address := regexp.MustCompile(`"@myriaddreamin/[^"]*":\s*"([^"]*)"`)

	for _, page := range []struct {
		name   string
		render func(*bytes.Buffer) error
	}{
		{"index", func(out *bytes.Buffer) error { return Index(out, Site{}) }},
		{"station", func(out *bytes.Buffer) error {
			return Station(out, "beispiel", timetable.Day{Station: "Beispiel"}, Site{}, nil)
		}},
		{"legal", func(out *bytes.Buffer) error { return Legal(out, Site{}, "Impressum", []byte("<p>x</p>")) }},
	} {
		var out bytes.Buffer
		if err := page.render(&out); err != nil {
			t.Fatalf("%s: %v", page.name, err)
		}
		matches := address.FindAllStringSubmatch(out.String(), -1)
		if page.name != "legal" && len(matches) == 0 {
			t.Errorf("%s: no import map at all", page.name)
		}
		for _, match := range matches {
			if got := match[1]; !strings.HasPrefix(got, "./") && !strings.HasPrefix(got, "../") && !strings.HasPrefix(got, "/") {
				t.Errorf("%s: import map address %q is bare, so the browser discards it", page.name, got)
			}
		}
	}
}

// The footer links have to work from both depths too.
func TestFooterLinksAreRelative(t *testing.T) {
	var out bytes.Buffer
	if err := Station(&out, "beispiel", timetable.Day{Station: "Beispiel"}, Site{HasImpressum: true}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `href="../impressum.html"`) {
		t.Error("a station page should reach the Impressum with ../")
	}
}
