package dto_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Paca-AI/api/internal/transport/http/dto"
)

// ---------------------------------------------------------------------------
// CustomFieldOptionDTO.UnmarshalJSON
// ---------------------------------------------------------------------------

// TestCustomFieldOptionDTO_UnmarshalJSON_PlainString is a regression test for
// a PR review finding: a direct API caller still sending the pre-000050
// plain-string options shape (e.g. ["Open","Closed"]) must not get a hard
// decode failure now that options carry an optional color, matching the
// same two-shape tolerance already applied by the MCP tool layer and the
// repository's legacy-row unmarshaling.
func TestCustomFieldOptionDTO_UnmarshalJSON_PlainString(t *testing.T) {
	var opt dto.CustomFieldOptionDTO
	if err := json.Unmarshal([]byte(`"Open"`), &opt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := dto.CustomFieldOptionDTO{Value: "Open"}
	if !reflect.DeepEqual(opt, want) {
		t.Fatalf("opt = %+v, want %+v", opt, want)
	}
}

func TestCustomFieldOptionDTO_UnmarshalJSON_ObjectWithColor(t *testing.T) {
	var opt dto.CustomFieldOptionDTO
	if err := json.Unmarshal([]byte(`{"value":"High","color":"#ef4444"}`), &opt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	red := "#ef4444"
	want := dto.CustomFieldOptionDTO{Value: "High", Color: &red}
	if !reflect.DeepEqual(opt, want) {
		t.Fatalf("opt = %+v, want %+v", opt, want)
	}
}

func TestCustomFieldOptionDTO_UnmarshalJSON_ObjectWithoutColor(t *testing.T) {
	var opt dto.CustomFieldOptionDTO
	if err := json.Unmarshal([]byte(`{"value":"Low"}`), &opt); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := dto.CustomFieldOptionDTO{Value: "Low"}
	if !reflect.DeepEqual(opt, want) {
		t.Fatalf("opt = %+v, want %+v", opt, want)
	}
}

func TestCustomFieldOptionDTO_UnmarshalJSON_MixedArray(t *testing.T) {
	var opts []dto.CustomFieldOptionDTO
	if err := json.Unmarshal([]byte(`["Low",{"value":"High","color":"#ef4444"}]`), &opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	red := "#ef4444"
	want := []dto.CustomFieldOptionDTO{{Value: "Low"}, {Value: "High", Color: &red}}
	if !reflect.DeepEqual(opts, want) {
		t.Fatalf("opts = %+v, want %+v", opts, want)
	}
}

func TestCustomFieldOptionDTO_UnmarshalJSON_InvalidType(t *testing.T) {
	var opt dto.CustomFieldOptionDTO
	if err := json.Unmarshal([]byte(`42`), &opt); err == nil {
		t.Fatal("expected an error for a non-string, non-object element")
	}
}

// TestCreateCustomFieldDefinitionRequest_LegacyPlainStringOptions verifies
// the fix end-to-end: a full request body using the old plain-string
// options array still decodes successfully.
func TestCreateCustomFieldDefinitionRequest_LegacyPlainStringOptions(t *testing.T) {
	body := []byte(`{
		"field_key": "priority",
		"display_name": "Priority",
		"field_type": "select",
		"options": ["Low", "High"]
	}`)
	var req dto.CreateCustomFieldDefinitionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := req.CustomFieldOptionsToDomain()
	if len(got) != 2 || got[0].Value != "Low" || got[1].Value != "High" {
		t.Fatalf("got = %+v, want [{Low <nil>} {High <nil>}]", got)
	}
}
