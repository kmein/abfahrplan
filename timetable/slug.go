package timetable

import "strings"

// transliterate covers every non-ASCII letter that appears in station names in
// the VBB feed. Spelling them out rather than stripping the accent means
// searching for "koepenick" finds "Köpenick", which is how people type.
var transliterate = map[rune]string{
	'ä': "ae", 'ö': "oe", 'ü': "ue", 'ß': "ss",
	'é': "e", 'è': "e", 'ó': "o",
}

// Slug turns a station name into a URL path segment.
func Slug(name string) string {
	var slug strings.Builder
	dash := true // starts true so leading separators are dropped
	for _, r := range strings.ToLower(name) {
		if spelled, ok := transliterate[r]; ok {
			slug.WriteString(spelled)
			dash = false
			continue
		}
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			slug.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			slug.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(slug.String(), "-")
}
