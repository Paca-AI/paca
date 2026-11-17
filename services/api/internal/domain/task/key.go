package taskdom

import (
	"regexp"
	"strconv"
	"strings"
)

var taskKeyRe = regexp.MustCompile(`^([A-Za-z0-9]{1,10})-([0-9]+)$`)

// ParseTaskKey parses a task key such as "PRJ-1" or "prj-42" into its
// uppercase project prefix and positive task number.
func ParseTaskKey(raw string) (prefix string, number int64, ok bool) {
	m := taskKeyRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.ParseInt(m[2], 10, 64)
	if err != nil || n < 1 {
		return "", 0, false
	}
	return strings.ToUpper(m[1]), n, true
}
