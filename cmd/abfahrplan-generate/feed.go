package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fetchFeed downloads the GTFS feed to dest, unless the server says it has not
// changed since the copy we already have. Reports whether dest now holds
// something new.
//
// VBB publishes a new feed every few weeks, so a daily timer should cost one
// conditional request on almost every run and nothing else.
func fetchFeed(url, dest string) (bool, error) {
	stamp := dest + ".etag"

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("User-Agent", "abfahrplan (+https://github.com/kmein/abfahrplan)")

	if known, err := os.ReadFile(stamp); err == nil {
		if _, already := os.Stat(dest); already == nil {
			validator := strings.TrimSpace(string(known))
			if strings.HasPrefix(validator, "W/") || strings.HasPrefix(validator, `"`) {
				request.Header.Set("If-None-Match", validator)
			} else {
				request.Header.Set("If-Modified-Since", validator)
			}
		}
	}

	client := &http.Client{Timeout: 30 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotModified {
		log("feed unchanged since the last run")
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetching %s: %s", url, response.Status)
	}
	// A feed server having a bad day serves an HTML error page with status 200.
	if kind := response.Header.Get("Content-Type"); kind != "" && !strings.Contains(kind, "zip") && !strings.Contains(kind, "octet-stream") {
		return false, fmt.Errorf("fetching %s: expected a zip, got %s", url, kind)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, err
	}
	partial := dest + ".part"
	file, err := os.Create(partial)
	if err != nil {
		return false, err
	}

	written, err := io.Copy(file, response.Body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(partial)
		return false, err
	}
	// Two zip end-of-directory records plus a header is already more than this;
	// anything smaller is an error page wearing a zip's content type.
	if written < 1024 {
		os.Remove(partial)
		return false, fmt.Errorf("fetching %s: only %d bytes, that is not a feed", url, written)
	}
	if err := os.Rename(partial, dest); err != nil {
		return false, err
	}

	if validator := response.Header.Get("ETag"); validator != "" {
		os.WriteFile(stamp, []byte(validator), 0o644)
	} else if validator := response.Header.Get("Last-Modified"); validator != "" {
		os.WriteFile(stamp, []byte(validator), 0o644)
	}

	log("fetched %s (%.0f MB)", url, float64(written)/(1<<20))
	return true, nil
}
