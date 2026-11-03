package gitprovider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Core keeps its own copy of the SPI wire types (it does not import the SDK).
// This test asserts the copy still has exactly the JSON shape the SDK's own
// contract test recorded in the shared golden file. When it fails, the SDK
// changed (or this copy did): run scripts/sync-git-spi.sh.

func shapeOf(t reflect.Type) any { return shapeOfSeen(t, map[reflect.Type]bool{}) }

func shapeOfSeen(t reflect.Type, seen map[reflect.Type]bool) any {
	switch t.Kind() { //nolint:exhaustive // every other kind is described by its own name
	case reflect.Pointer:
		return shapeOfSeen(t.Elem(), seen)
	case reflect.Struct:
		if t.PkgPath() == "time" {
			return "time"
		}
		if seen[t] {
			return "recursive:" + t.Name()
		}
		seen[t] = true
		defer delete(seen, t)
		out := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" || !f.IsExported() {
				continue
			}
			if name == "" {
				name = f.Name
			}
			out[name] = shapeOfSeen(f.Type, seen)
		}
		return out
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytes"
		}
		return []any{shapeOfSeen(t.Elem(), seen)}
	case reflect.Map:
		return map[string]any{"*": shapeOfSeen(t.Elem(), seen)}
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Interface:
		return "any"
	}
	return t.Kind().String()
}

func TestIssueListWireContract(t *testing.T) {
	for _, name := range []string{"ListIssuesRequest", "IssuePage"} {
		if _, ok := WireTypes()[name]; !ok {
			t.Errorf("missing wire type %s", name)
		}
	}
	b, err := json.Marshal(Capabilities{})
	if err != nil || strings.Contains(string(b), `"issue_list"`) {
		t.Fatalf("optional capability: %s %v", b, err)
	}
	b, err = json.Marshal(Capabilities{IssueList: true})
	if err != nil || !strings.Contains(string(b), `"issue_list":true`) {
		t.Fatalf("enabled capability: %s %v", b, err)
	}
}

func TestWireShapesMatchTheSDKContract(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "wire-shapes.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{"spi_version": SPIVersion}
	shapes := map[string]any{}
	for name, typ := range WireTypes() {
		shapes[name] = shapeOf(typ)
	}
	got["types"] = shapes
	raw, _ := json.MarshalIndent(got, "", "  ")
	raw = append(raw, '\n')
	if string(raw) != string(want) {
		t.Fatal("core's copy of the Git provider SPI types differs from the SDK contract (testdata/wire-shapes.golden.json). Run scripts/sync-git-spi.sh from the repository root.")
	}
}
