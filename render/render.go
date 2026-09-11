// Package render turns a timetable into a PDF by running Typst over the
// embedded template.
package render

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Template travels inside the binary so a packaged service has no relative
// path to resolve and no separate file to install.
//
//go:embed timetable.typ
var Template []byte

// Renderer runs Typst. The zero value works if typst is on PATH; a Renderer is
// safe for concurrent use, since each render gets its own directory.
type Renderer struct {
	// Typst is the binary to run. Defaults to $ABFAHRPLAN_TYPST, else "typst".
	Typst string
	// FontPath is searched for the document's fonts. Defaults to
	// $ABFAHRPLAN_FONT_PATH. When set, system fonts are ignored, so a service
	// renders the same sheet wherever it runs.
	FontPath string
	// Timeout bounds a single render. Defaults to 60s.
	Timeout time.Duration
}

func (r *Renderer) typst() string {
	if r.Typst != "" {
		return r.Typst
	}
	if env := os.Getenv("ABFAHRPLAN_TYPST"); env != "" {
		return env
	}
	return "typst"
}

func (r *Renderer) fontPath() string {
	if r.FontPath != "" {
		return r.FontPath
	}
	return os.Getenv("ABFAHRPLAN_FONT_PATH")
}

func (r *Renderer) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return 60 * time.Second
}

// PDF renders one timetable. day is anything that marshals to the shape
// timetable.typ expects, i.e. a timetable.Day.
func (r *Renderer) PDF(ctx context.Context, day any) ([]byte, error) {
	data, err := json.Marshal(day)
	if err != nil {
		return nil, fmt.Errorf("marshalling timetable: %w", err)
	}

	// The template reads timetable.json from its own directory, which is also
	// what the CLI does by hand, so give each render a directory holding both.
	dir, err := os.MkdirTemp("", "abfahrplan-render-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	if err := os.WriteFile(filepath.Join(dir, "timetable.json"), data, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "timetable.typ"), Template, 0o644); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	args := []string{"compile", "--root", dir}
	if fonts := r.fontPath(); fonts != "" {
		args = append(args, "--font-path", fonts, "--ignore-system-fonts")
	}
	args = append(args, filepath.Join(dir, "timetable.typ"), "-")

	cmd := exec.CommandContext(ctx, r.typst(), args...)
	// typst must never reach out for a package; the document uses none.
	cmd.Env = append(os.Environ(), "TYPST_PACKAGE_PATH="+dir, "TYPST_PACKAGE_CACHE_PATH="+dir)
	cmd.WaitDelay = 5 * time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("typst: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// Check renders a fixture and reports what fonts came out, so a caller can
// refuse to start rather than quietly produce sheets in a fallback face: typst
// substitutes a missing font without erroring.
func (r *Renderer) Check(ctx context.Context) ([]byte, error) {
	return r.PDF(ctx, map[string]any{
		"station": "Beispielhausen, Schule",
		"hours": []any{map[string]any{
			"hour": 7,
			"directions": []any{map[string]any{
				"route_short":      "412",
				"direction":        0,
				"departuresMonFri": []any{map[string]any{"minute": 12, "headsign": "Musterdorf"}},
				"departuresSat":    []any{},
				"departuresSun":    []any{},
			}},
		}},
	})
}
