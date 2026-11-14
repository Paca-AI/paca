package docdom

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
)

func TestChangeCursor_RoundTrip(t *testing.T) {
	in := ChangeCursor{ProjectID: uuid.New(), Seq: 42, Fingerprint: "d41d8cd98f00b204e9800998ecf8427e"}
	out, ok := DecodeChangeCursor(EncodeChangeCursor(in))
	if !ok || out != in {
		t.Fatalf("round trip gave %+v (ok %v), want %+v", out, ok, in)
	}
	// The same position always encodes to the same string, so a client can
	// compare cursors to tell "nothing changed".
	first, second := EncodeChangeCursor(in), EncodeChangeCursor(out)
	if first != second {
		t.Fatalf("the same position encoded as %q and %q", first, second)
	}
}

func TestChangeCursor_RejectsAnythingElse(t *testing.T) {
	valid := EncodeChangeCursor(ChangeCursor{ProjectID: uuid.New(), Seq: 7, Fingerprint: "abc"})
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	cases := map[string]string{
		"empty":             "",
		"garbage":           "not a cursor!",
		"truncated":         valid[:len(valid)/2],
		"not json":          b64("hello"),
		"json array":        b64(`[1,2,3]`),
		"missing project":   b64(`{"s":1,"f":"x"}`),
		"nil project":       b64(`{"p":"00000000-0000-0000-0000-000000000000","s":1,"f":"x"}`),
		"bad project":       b64(`{"p":"nope","s":1,"f":"x"}`),
		"negative seq":      b64(`{"p":"7b0a3c0e-51a5-4a0e-9a57-0c9a3f2d1e11","s":-1,"f":"x"}`),
		"seq not a number":  b64(`{"p":"7b0a3c0e-51a5-4a0e-9a57-0c9a3f2d1e11","s":"1","f":"x"}`),
		"missing fingerpr.": b64(`{"p":"7b0a3c0e-51a5-4a0e-9a57-0c9a3f2d1e11","s":1}`),
		"trailing data":     b64(`{"p":"7b0a3c0e-51a5-4a0e-9a57-0c9a3f2d1e11","s":1,"f":"x"} {}`),
		"padded base64":     valid + "==",
		"huge":              b64(`{"p":"7b0a3c0e-51a5-4a0e-9a57-0c9a3f2d1e11","s":1,"f":"` + string(make([]byte, 5000)) + `"}`),
	}
	for name, raw := range cases {
		if c, ok := DecodeChangeCursor(raw); ok {
			t.Errorf("%s: decoded to %+v, want a rejection", name, c)
		}
	}
}
