package docdom

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Paca-AI/go-blocknote2md"
)

// maxFileNameBytes bounds one file name segment (file systems cap names near 255).
const maxFileNameBytes = 100

var unsafeFileNameChars = regexp.MustCompile(`[\x00-\x1f\x7f/\\:*?"<>|]+`)

// SafeFileSegment turns a title into one safe file name segment: no separators
// or control characters, never "." / "..", never empty, bounded in length. It
// keeps a hostile title from escaping a directory when a file is extracted.
func SafeFileSegment(name string) string {
	s := strings.TrimSpace(unsafeFileNameChars.ReplaceAllString(name, " "))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if len(s) > maxFileNameBytes {
		s = s[:maxFileNameBytes]
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

// MarkdownFileName is the name a document downloads as: its title and ".md".
func MarkdownFileName(title string) string { return SafeFileSegment(title) + ".md" }

// bodyMarkdown converts a document body with BlockNote's own Markdown algorithm
// (github.com/Paca-AI/go-blocknote2md). A body that isn't a block array renders
// as empty, so one odd document never fails an export.
func bodyMarkdown(content []byte, origin string) string {
	md, err := blocknote2md.Convert(content, blocknote2md.WithOrigin(origin))
	if err != nil {
		return ""
	}
	return md
}

// RenderMarkdown renders a document as the Markdown file it downloads as: its
// title as the level-1 heading the file starts with, then its body. Importing
// such a file takes that heading back as the title. origin is the app's public
// URL, which relative links in the body resolve against.
func RenderMarkdown(title string, content []byte, origin string) string {
	heading, _ := json.Marshal([]any{map[string]any{
		"type":    "heading",
		"props":   map[string]any{"level": 1},
		"content": []any{map[string]any{"type": "text", "text": title, "styles": map[string]any{}}},
	}})
	out := bodyMarkdown(heading, origin)
	if md := bodyMarkdown(content, origin); md != "" {
		out += "\n" + md
	}
	return out
}
