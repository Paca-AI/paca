package iam

import "strings"

// ParseResource splits a resource name into its kind, its own id and the id
// of the project that contains it:
//
//	project/P/agent/A        -> ("agent", "A", "P")
//	project/P/task/T/x/y     -> ("task", "T", "P")   (deeper segments ignored)
//	project/P                -> ("project", "P", "P")
//	project/P/task           -> ("task", "", "P")
//	agent/A                  -> ("agent", "A", "")
//	settings                 -> ("settings", "", "")
//	"*", "" or any name with an empty segment -> ("", "", "")
//
// Ids may be "*" (a list/create check against a not-yet-existing resource);
// callers must not treat "*" as a loadable id.
func ParseResource(resource string) (kind, id, projectID string) {
	if resource == "" || resource == "*" {
		return "", "", ""
	}
	segs := strings.Split(resource, "/")
	for _, s := range segs {
		if s == "" {
			return "", "", ""
		}
	}
	if segs[0] != "project" {
		if len(segs) >= 2 {
			return segs[0], segs[1], ""
		}
		return segs[0], "", ""
	}
	switch len(segs) {
	case 1:
		return "project", "", ""
	case 2:
		return "project", segs[1], segs[1]
	case 3:
		return segs[2], "", segs[1]
	default:
		return segs[2], segs[3], segs[1]
	}
}
