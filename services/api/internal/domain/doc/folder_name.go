package docdom

import "strings"

// ValidFolderName reports whether name can be a documentation folder name:
// something other than spaces, and no "/" (the path separator) or a backslash (folders
// live in the file manager's tree, whose names may not contain either).
func ValidFolderName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !strings.ContainsAny(name, `/\`)
}
