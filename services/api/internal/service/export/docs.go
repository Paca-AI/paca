package exportsvc

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Paca-AI/go-blocknote2md"
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

var unsafeNameChars = regexp.MustCompile(`[\x00-\x1f\x7f/\\:*?"<>|]+`)

// safeSegment turns a folder or document title into one safe path segment: no
// separators or control characters, never "." / "..", never empty, bounded in
// length. This is what keeps a hostile title from escaping docs/ when the zip
// is extracted.
func safeSegment(name string) string {
	s := strings.TrimSpace(unsafeNameChars.ReplaceAllString(name, " "))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if len(s) > maxNameBytes {
		s = s[:maxNameBytes]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s = strings.TrimSpace(s)
	}
	if s == "" {
		return "untitled"
	}
	return s
}

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

		body := titleHeading(d.Title, origin)
		if md := docMarkdown(d.Content, origin); md != "" {
			body += "\n" + md
		}
		out = append(out, docFile{Path: path, Body: body})
	}
	return out
}

// docMarkdown converts a document body with BlockNote's own Markdown
// algorithm (github.com/Paca-AI/go-blocknote2md). A body that isn't a block
// array exports as empty, so one odd document never fails a whole export.
func docMarkdown(content []byte, origin string) string {
	md, err := blocknote2md.Convert(content, blocknote2md.WithOrigin(origin))
	if err != nil {
		return ""
	}
	return md
}

// titleHeading renders a document's title as the level-1 heading its file
// starts with, through the same converter as the body.
func titleHeading(title, origin string) string {
	block, _ := json.Marshal([]any{map[string]any{
		"type":    "heading",
		"props":   map[string]any{"level": 1},
		"content": []any{map[string]any{"type": "text", "text": title, "styles": map[string]any{}}},
	}})
	return docMarkdown(block, origin)
}
