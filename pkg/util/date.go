package util

import (
	"strings"
	"time"
)

// formatDateTpl formats a given timestamp in milliseconds since the Unix epoch
// using a template with placeholders.
//
// Supported placeholders:
// - YYYY: 4-digit year
// - YY: 2-digit year
// - MM: 2-digit month (01-12)
// - DD: 2-digit day (01-31)
// - hh: 2-digit hour (00-23)
// - mm: 2-digit minute (00-59)
// - ss: 2-digit second (00-59)
//
// Parameters:
// - ts: timestamp in milliseconds since Unix epoch.
// - tpl: template string using placeholders.
//
// Returns:
// - A formatted date string according to the template.
// - An empty string if ts == 0.
//
// Example:
//
//	ts := int64(1699603200000)
//	formatDateTpl(ts, "YYYY.MM.DD")       // "2023.11.10"
//	formatDateTpl(ts, "DD/MM/YYYY")       // "10/11/2023"
//	formatDateTpl(ts, "YYYY-MM-DD hh:mm") // "2023-11-10 00:00"
func FormatDateTpl(ts int64, tpl string) string {
	if ts == 0 {
		return ""
	}

	goTpl := tpl
	replacements := map[string]string{
		"YYYY": "2006",
		"YY":   "06",
		"MM":   "01",
		"DD":   "02",
		"hh":   "15",
		"mm":   "04",
		"ss":   "05",
	}
	for k, v := range replacements {
		goTpl = strings.ReplaceAll(goTpl, k, v)
	}

	t := time.UnixMilli(ts)
	return t.Format(goTpl)
}
