package exportsvc

import (
	"strconv"
	"strings"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

// maxFolderDepth bounds the walk up a folder's parents, so a corrupt parent
// cycle can't loop forever.
const maxFolderDepth = 32

// maxNameBytes bounds one path segment (file systems cap names near 255).
const maxNameBytes = 100

// docFile is one document rendered into the archive.
type docFile struct {
	Path string // zip entry name, always under "docs/"
	Body string
}

// safeSegment turns a folder or document title into one safe path segment (see
// docdom.SafeFileSegment): what keeps a hostile title from escaping docs/ when
// the zip is extracted.
func safeSegment(name string) string { return docdom.SafeFileSegment(name) }

// buildDocFiles lays the project's documents out as Markdown files in the same
// folder tree they have in the app. Two documents that would land on the same
// path (same title in one folder, or titles differing only by case, which
// case-insensitive file systems treat as equal) get a numeric suffix.
func buildDocFiles(folders []*docdom.DocFolder, docs []*docdom.Document, origin string) []docFile {
	byID := make(map[uuid.UUID]*docdom.DocFolder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}

	dirOf := func(id *uuid.UUID) string {
		var segs []string
		seen := map[uuid.UUID]bool{}
		for cur := id; cur != nil && len(segs) < maxFolderDepth; {
			f, ok := byID[*cur]
			if !ok || seen[f.ID] {
				break
			}
			seen[f.ID] = true
			segs = append([]string{safeSegment(f.Name)}, segs...)
			cur = f.ParentID
		}
		return strings.Join(append([]string{"docs"}, segs...), "/")
	}

	used := map[string]bool{}
	out := make([]docFile, 0, len(docs))
	for _, d := range docs {
		dir := dirOf(d.FolderID)
		base := safeSegment(d.Title)
		path := dir + "/" + base + ".md"
		for n := 2; used[strings.ToLower(path)]; n++ {
			path = dir + "/" + base + " (" + strconv.Itoa(n) + ").md"
		}
		used[strings.ToLower(path)] = true

		out = append(out, docFile{Path: path, Body: docdom.RenderMarkdown(d.Title, d.Content, origin)})
	}
	return out
}
