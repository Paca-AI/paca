package exportsvc

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	docdom "github.com/Paca-AI/api/internal/domain/doc"
)

func TestSafeSegment(t *testing.T) {
	for in, want := range map[string]string{
		"Plain title":      "Plain title",
		"a/b\\c":           "a b c",
		"..":               "untitled",
		".":                "untitled",
		"../../etc/passwd": "etc passwd",
		"  spaced   out ":  "spaced out",
		"":                 "untitled",
		`we"ird:na*me?`:    "we ird na me",
		"tab\tand\nnl":     "tab and nl",
		"trailing dots...": "trailing dots",
	} {
		if got := safeSegment(in); got != want {
			t.Errorf("safeSegment(%q) = %q, want %q", in, got, want)
		}
	}
	if got := safeSegment(strings.Repeat("é", 200)); len(got) > maxNameBytes || !strings.HasPrefix(got, "é") {
		t.Errorf("long name not trimmed on a rune boundary: %d bytes", len(got))
	}
}

func TestBuildDocFiles_PathsAndCollisions(t *testing.T) {
	root := &docdom.DocFolder{ID: uuid.New(), Name: "Guides"}
	child := &docdom.DocFolder{ID: uuid.New(), Name: "../Setup", ParentID: &root.ID}
	// A parent cycle must terminate.
	a := &docdom.DocFolder{ID: uuid.New(), Name: "A"}
	b := &docdom.DocFolder{ID: uuid.New(), Name: "B", ParentID: &a.ID}
	a.ParentID = &b.ID

	files := buildDocFiles(
		[]*docdom.DocFolder{root, child, a, b},
		[]*docdom.Document{
			{ID: uuid.New(), Title: "Intro"},
			{ID: uuid.New(), Title: "intro"}, // same name modulo case
			{ID: uuid.New(), Title: "Intro"},
			{ID: uuid.New(), Title: "Deep", FolderID: &child.ID},
			{ID: uuid.New(), Title: "../../escape"},
			{ID: uuid.New(), Title: "Cyclic", FolderID: &a.ID},
			{ID: uuid.New(), Title: "Orphan", FolderID: ptr(uuid.New())},
		}, "")

	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
		if strings.Contains(f.Path, "..") || !strings.HasPrefix(f.Path, "docs/") {
			t.Errorf("unsafe path %q", f.Path)
		}
	}
	want := []string{
		"docs/Intro.md", "docs/intro (2).md", "docs/Intro (3).md",
		"docs/Guides/Setup/Deep.md", "docs/escape.md",
	}
	for _, w := range want {
		if !contains(paths, w) {
			t.Errorf("missing %q in %v", w, paths)
		}
	}
	if len(files) != 7 {
		t.Errorf("got %d files, want 7: %v", len(files), paths)
	}
}

func ptr[T any](v T) *T { return &v }
