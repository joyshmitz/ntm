package sqliteutil

import (
	"net/url"
	"strings"

	_ "modernc.org/sqlite"
)

const DriverName = "sqlite"

func FileDSN(path string, pragmas ...string) string {
	q := pragmaQuery(pragmas...)
	return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
}

func MemoryDSN(pragmas ...string) string {
	q := pragmaQuery(pragmas...)
	if encoded := q.Encode(); encoded != "" {
		return "file::memory:?" + encoded
	}
	return "file::memory:"
}

func pragmaQuery(pragmas ...string) url.Values {
	q := url.Values{}
	q.Set("_time_format", "sqlite")
	q.Set("_timezone", "UTC")
	for _, pragma := range pragmas {
		pragma = strings.TrimSpace(pragma)
		if pragma == "" {
			continue
		}
		q.Add("_pragma", pragma)
	}
	return q
}
