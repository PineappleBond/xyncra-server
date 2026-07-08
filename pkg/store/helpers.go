package store

import "strings"

// escapeLikePattern escapes special LIKE characters (%, _, |) in the input so
// they are treated as literal characters in LIKE expressions. The pipe
// character '|' is used as the escape character (passed via ESCAPE '|' in the
// SQL query) because it works consistently across SQLite, PostgreSQL, and
// MySQL — unlike backslash, whose escaping rules vary by dialect.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "|", "||")
	s = strings.ReplaceAll(s, "%", "|%")
	s = strings.ReplaceAll(s, "_", "|_")
	return s
}
