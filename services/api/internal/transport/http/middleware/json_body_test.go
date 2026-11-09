package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// jsonFoldKey must agree with encoding/json's own struct-field matching: a key
// folds to a field's name exactly when the decoder applies it to that field.
// If a Go release changes the matching, this fails instead of the
// authorization gates silently missing a spelling the handlers accept.
func TestJSONFoldKeyMatchesEncodingJSON(t *testing.T) {
	type dst struct {
		StatusID string `json:"status_id"`
		Kind     string `json:"kind"`
	}
	keys := []string{
		"status_id", "STATUS_ID", "Status_Id", "sTaTuS_iD",
		"ſtatus_id", // long s: folds to S
		"status_ıd", // dotless i: folds to I
		"Kind",      // Kelvin sign: folds to K
		"KIND", "kind",
		"status-id", "statusid", "status_id ", " status_id", "status_idx",
		"ßtatus_id", // sharp s: no simple fold to s
	}
	for _, k := range keys {
		var d dst
		if err := json.Unmarshal([]byte(`{`+strconv.Quote(k)+`:"x"}`), &d); err != nil {
			t.Fatalf("%q: %v", k, err)
		}
		for field, applied := range map[string]bool{"status_id": d.StatusID == "x", "kind": d.Kind == "x"} {
			if folds := jsonFoldKey(k) == field; folds != applied {
				t.Errorf("key %q: jsonFoldKey gives %q, but encoding/json applied it to %s: %v", k, jsonFoldKey(k), field, applied)
			}
		}
	}
}

func TestFoldBodyKeys(t *testing.T) {
	got, err := foldBodyKeys(map[string]json.RawMessage{"Status_ID": json.RawMessage(`"a"`), "title": json.RawMessage(`"t"`)})
	if err != nil || string(got["status_id"]) != `"a"` || string(got["title"]) != `"t"` || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := foldBodyKeys(map[string]json.RawMessage{"status_id": nil, "STATUS_ID": nil}); err == nil {
		t.Fatal("two keys folding alike must be refused")
	}
	if got, err := foldBodyKeys(nil); got != nil || err != nil {
		t.Fatalf("nil body: got %v, %v", got, err)
	}
}

func TestDecodeSingleJSON(t *testing.T) {
	for body, want := range map[string]error{
		`{"a":1}`:          nil,
		"{\"a\":1}\n \t\r": nil,
		`{"a":1} {}`:       errTrailingJSON,
		`{"a":1}x`:         errTrailingJSON,
		`{"a":1}}`:         errTrailingJSON,
		`{"a":1} null`:     errTrailingJSON,
	} {
		var v map[string]any
		if err := decodeSingleJSON(strings.NewReader(body), &v); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", body, err, want)
		}
	}
	var v map[string]any
	if err := decodeSingleJSON(strings.NewReader(`not-json`), &v); err == nil || errors.Is(err, errTrailingJSON) {
		t.Errorf("invalid JSON: got %v, want a syntax error", err)
	}
}

func TestBindJSON_RejectsTrailingData(t *testing.T) {
	for body, wantOK := range map[string]bool{
		`{"name":"alice"} {"name":"bob"}`: false,
		`{"name":"alice"}garbage`:         false,
		"{\"name\":\"alice\"}\n":          true,
	} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/bind", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		var dst bindReq
		if ok := BindJSON(w, req, &dst); ok != wantOK {
			t.Errorf("%q: BindJSON ok=%v, want %v (status %d)", body, ok, wantOK, w.Code)
		} else if !ok && w.Code != http.StatusBadRequest {
			t.Errorf("%q: got status %d, want 400", body, w.Code)
		}
	}
}
