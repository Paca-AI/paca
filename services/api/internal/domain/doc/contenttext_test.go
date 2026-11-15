package docdom

import (
	"encoding/json"
	"testing"
)

func TestContentText(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"nil", ``, ""},
		{"null", `null`, ""},
		{"empty array", `[]`, ""},
		{"invalid json", `not json`, ""},
		{
			"bold run joins its neighbours",
			`[{"type":"paragraph","content":[
				{"type":"text","text":"foo ","styles":{}},
				{"type":"text","text":"bar","styles":{"bold":true}}]}]`,
			"foo bar",
		},
		{
			"one line per block, children after their parent, ids optional",
			`[
			 {"id":"1","type":"bulletListItem","content":[{"type":"text","text":"one"}],"children":[
			   {"type":"bulletListItem","content":[{"type":"text","text":"one.a"}],"children":[
			     {"type":"bulletListItem","content":[{"type":"text","text":"one.a.i"}]}]},
			   {"type":"bulletListItem","content":[{"type":"text","text":"one.b"}]}]},
			 {"type":"bulletListItem","content":[{"type":"text","text":"two"}],"children":[]}
			]`,
			"one\none.a\none.a.i\none.b\ntwo",
		},
		{
			"empty block keeps its line",
			`[{"type":"paragraph","content":[{"type":"text","text":"a"}]},
			  {"type":"paragraph","content":[]},
			  {"type":"paragraph","content":[{"type":"text","text":"b"}]}]`,
			"a\n\nb",
		},
		{
			"link keeps its text",
			`[{"type":"paragraph","content":[
				{"type":"text","text":"see "},
				{"type":"link","href":"https://example.com","content":[{"type":"text","text":"the docs","styles":{"italic":true}}]},
				{"type":"text","text":" now"}]}]`,
			"see the docs now",
		},
		{
			"table is one line, cells and rows separated by tabs",
			`[{"type":"table","content":{"type":"tableContent","columnWidths":[null,null],"rows":[
				{"cells":[[{"type":"text","text":"a1"}],[{"type":"text","text":"b1"}]]},
				{"cells":[
					{"type":"tableCell","props":{},"content":[{"type":"text","text":"a2"}]},
					{"type":"tableCell","props":{},"content":[{"type":"text","text":"b"},{"type":"text","text":"2","styles":{"bold":true}}]}]}
			]}},
			{"type":"paragraph","content":[{"type":"text","text":"after"}]}]`,
			"a1\tb1\ta2\tb2\nafter",
		},
		{
			"a hard break inside a run does not start a new line",
			`[{"type":"paragraph","content":[{"type":"text","text":"line one\nline two"}]}]`,
			"line one line two",
		},
		{
			"structural keys are not text",
			`[{"type":"heading","props":{"level":1,"text":"no"},"content":[{"type":"text","text":"Title"}]}]`,
			"Title",
		},
		{
			"top level not an array: every text string, in order, one per line",
			`{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"pm "},{"type":"text","text":"shape","marks":[{"type":"bold"}]}]},
				{"type":"paragraph","attrs":{"text":"attr"},"content":[{"type":"text","text":"second\nline"}]}]}`,
			"pm \nshape\nattr\nsecond line",
		},
		{"top level a bare string", `"just text"`, ""},
		{"top level object without text", `{"type":"doc"}`, ""},
		{
			"plain string content",
			`[{"type":"paragraph","content":"just a string"}]`,
			"just a string",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var in json.RawMessage
			if tc.content != "" {
				in = json.RawMessage(tc.content)
			}
			if got := ContentText(in); got != tc.want {
				t.Fatalf("ContentText = %q, want %q", got, tc.want)
			}
		})
	}
}
