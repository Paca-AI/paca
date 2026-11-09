package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Paca-AI/api/internal/apierr"
)

// errTrailingJSON reports data after the one JSON value a request body holds.
var errTrailingJSON = errors.New("request body must hold a single JSON value")

// decodeSingleJSON decodes exactly one JSON value from r into dst. Anything
// but whitespace after that value is errTrailingJSON. Handlers (BindJSON) and
// the authorization gates that peek at a body (RequireRequestAttrs and the
// bulk-item gates) both decode through it, so a gate can never read a shorter
// body than the handler applies.
func decodeSingleJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(r)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errTrailingJSON
	}
	return nil
}

// jsonFoldKey returns the form in which encoding/json matches an object key to
// a struct field (its foldName), lower-cased: ASCII letters fold by case and
// every other rune folds to the smallest rune of its unicode.SimpleFold orbit.
// "STATUS_ID", "Status_Id" and "ſtatus_id" (long s) all fold to "status_id",
// and a handler decoding into a struct tagged `json:"status_id"` applies each
// of them. TestJSONFoldKeyMatchesEncodingJSON pins the two together.
func jsonFoldKey(k string) string {
	var b strings.Builder
	b.Grow(len(k))
	for _, r := range k {
		if r >= utf8.RuneSelf {
			r = smallestFold(r)
		}
		if 'A' <= r && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// smallestFold returns the smallest rune in r's case-fold orbit, as
// encoding/json's foldRune does.
func smallestFold(r rune) rune {
	for {
		r2 := unicode.SimpleFold(r)
		if r2 <= r {
			return r2
		}
		r = r2
	}
}

// foldBodyKeys re-keys a peeked body by jsonFoldKey, so an extractor looking
// up "status_id" sees every key the handler's struct decoding would apply to
// that field. Two keys that fold alike are refused: the handler would apply
// whichever comes last, and the gate must not authorize a different one.
func foldBodyKeys(body map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if body == nil {
		return nil, nil
	}
	out := make(map[string]json.RawMessage, len(body))
	for k, v := range body {
		fk := jsonFoldKey(k)
		if _, dup := out[fk]; dup {
			return nil, apierr.New(apierr.CodeBadRequest, "request body sets field "+strconv.Quote(fk)+" more than once")
		}
		out[fk] = v
	}
	return out, nil
}
