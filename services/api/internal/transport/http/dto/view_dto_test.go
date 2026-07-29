package dto_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	"github.com/Paca-AI/api/internal/transport/http/dto"
)

// ---------------------------------------------------------------------------
// ViewFromEntity roundtrip
// ---------------------------------------------------------------------------

// TestViewFromEntity_FiltersRoundtrip verifies that all ViewFilters fields
// using FilterConfig are correctly mapped to ViewResponse.
func TestViewFromEntity_FiltersRoundtrip(t *testing.T) {
	sprintID := uuid.New()
	projectID := uuid.New()
	viewID := uuid.New()
	statusID := uuid.NewString()
	assigneeID := uuid.NewString()

	entity := &sprintdom.SprintView{
		ID:        viewID,
		SprintID:  &sprintID,
		ProjectID: projectID,
		Name:      "Board",
		ViewType:  sprintdom.ViewTypeBoard,
		Position:  1,
		Config: sprintdom.ViewConfig{
			ColumnBy: "status",
			Filters: &sprintdom.ViewFilters{
				TaskTypes: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						"normal": sprintdom.FilterEntryNested(sprintdom.FilterConfig{All: true}),
					},
				},
				Sprints: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						sprintID.String(): sprintdom.FilterEntryInclude(),
						uuid.NewString():  sprintdom.FilterEntryInclude(),
					},
				},
				Statuses: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						statusID: sprintdom.FilterEntryInclude(),
					},
				},
				Assignees: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						assigneeID: sprintdom.FilterEntryInclude(),
					},
				},
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	resp := dto.ViewFromEntity(entity)

	if resp.Config.Filters == nil {
		t.Fatal("expected non-nil Filters in response")
	}
	f := resp.Config.Filters

	if f.TaskTypes == nil {
		t.Fatal("expected non-nil TaskTypes in response filters")
	}
	normalEntry, ok := f.TaskTypes.Items["normal"]
	if !ok {
		t.Fatal("expected 'normal' key in TaskTypes.Items")
	}
	if !normalEntry.IsNested() {
		t.Error("expected 'normal' entry to be a nested FilterConfig")
	}
	if normalEntry.Config() == nil || !normalEntry.Config().All {
		t.Error("expected 'normal' nested config to have All=true")
	}

	if f.Sprints == nil {
		t.Fatal("expected non-nil Sprints in response filters")
	}
	if len(f.Sprints.Items) != 2 {
		t.Errorf("Sprints.Items: want 2 entries, got %d", len(f.Sprints.Items))
	}
	if _, ok := f.Sprints.Items[sprintID.String()]; !ok {
		t.Errorf("Sprints.Items missing original sprintID %s", sprintID)
	}

	if f.Statuses == nil || len(f.Statuses.Items) != 1 {
		t.Error("Statuses.Items: want 1 entry")
	}
	if f.Assignees == nil || len(f.Assignees.Items) != 1 {
		t.Error("Assignees.Items: want 1 entry")
	}
}

// TestViewFromEntity_NilFilters verifies that a nil Filters field maps to nil
// in the DTO (no panic).
func TestViewFromEntity_NilFilters(t *testing.T) {
	entity := &sprintdom.SprintView{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Table",
		ViewType:  sprintdom.ViewTypeTable,
		Config:    sprintdom.ViewConfig{},
	}
	resp := dto.ViewFromEntity(entity)
	if resp.Config.Filters != nil {
		t.Errorf("expected nil Filters for entity with nil filters, got %+v", resp.Config.Filters)
	}
}

// TestViewFromEntity_PluginConfigRoundtrip verifies plugin fields are copied
// from domain config to DTO config.
func TestViewFromEntity_PluginConfigRoundtrip(t *testing.T) {
	entity := &sprintdom.SprintView{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Plugin View",
		ViewType:  sprintdom.ViewTypePlugin,
		Config: sprintdom.ViewConfig{
			PluginID:        uuid.NewString(),
			PluginComponent: "KanbanByPriority",
		},
	}

	resp := dto.ViewFromEntity(entity)

	if resp.Config.PluginManifestID != entity.Config.PluginID {
		t.Errorf("PluginManifestID: want %q, got %q", entity.Config.PluginID, resp.Config.PluginManifestID)
	}
	if resp.Config.PluginComponent != entity.Config.PluginComponent {
		t.Errorf("PluginComponent: want %q, got %q", entity.Config.PluginComponent, resp.Config.PluginComponent)
	}
}

// TestViewFromEntity_PageSizeRoundtrip verifies the page_size field is copied
// from domain config to DTO config.
func TestViewFromEntity_PageSizeRoundtrip(t *testing.T) {
	entity := &sprintdom.SprintView{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Table",
		ViewType:  sprintdom.ViewTypeTable,
		Config: sprintdom.ViewConfig{
			PageSize: 50,
		},
	}

	resp := dto.ViewFromEntity(entity)

	if resp.Config.PageSize != 50 {
		t.Errorf("PageSize: want 50, got %d", resp.Config.PageSize)
	}
}

// TestViewFromEntity_InitialPageSizeRoundtrip verifies the initial_page_size
// field is copied from domain config to DTO config, independently of
// PageSize.
func TestViewFromEntity_InitialPageSizeRoundtrip(t *testing.T) {
	entity := &sprintdom.SprintView{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Board",
		ViewType:  sprintdom.ViewTypeBoard,
		Config: sprintdom.ViewConfig{
			PageSize:        20,
			InitialPageSize: 10,
		},
	}

	resp := dto.ViewFromEntity(entity)

	if resp.Config.InitialPageSize != 10 {
		t.Errorf("InitialPageSize: want 10, got %d", resp.Config.InitialPageSize)
	}
	if resp.Config.PageSize != 20 {
		t.Errorf("PageSize: want 20, got %d", resp.Config.PageSize)
	}
}

// ---------------------------------------------------------------------------
// toViewConfig (via CreateViewRequest / UpdateViewRequest) roundtrip
// ---------------------------------------------------------------------------

// TestCreateViewRequest_ToCreateInput_FiltersRoundtrip exercises toViewConfig
// indirectly through the CreateViewRequest helper.
func TestCreateViewRequest_ToCreateInput_FiltersRoundtrip(t *testing.T) {
	sprintID := uuid.New()
	projectID := uuid.New()
	systemTypeID := uuid.NewString()

	req := dto.CreateViewRequest{
		Name:     "Board",
		ViewType: sprintdom.ViewTypeBoard,
		Config: &dto.ViewConfigDTO{
			ColumnBy: "status",
			Filters: &dto.ViewFiltersDTO{
				TaskTypes: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						"normal":     sprintdom.FilterEntryNested(sprintdom.FilterConfig{All: true}),
						systemTypeID: sprintdom.FilterEntryInclude(),
					},
				},
				Sprints: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						sprintID.String(): sprintdom.FilterEntryInclude(),
					},
				},
			},
		},
	}

	input := req.ToCreateInput(sprintID, projectID)

	f := input.Config.Filters
	if f == nil {
		t.Fatal("expected non-nil Filters in domain input")
	}
	if f.TaskTypes == nil {
		t.Fatal("expected non-nil TaskTypes in domain input filters")
	}
	if len(f.TaskTypes.Items) != 2 {
		t.Errorf("TaskTypes.Items: want 2, got %d", len(f.TaskTypes.Items))
	}
	normalEntry, ok := f.TaskTypes.Items["normal"]
	if !ok || !normalEntry.IsNested() {
		t.Error("expected 'normal' entry to be a nested FilterConfig")
	}
	sysEntry, ok := f.TaskTypes.Items[systemTypeID]
	if !ok || sysEntry.IsNested() || !sysEntry.Flag() {
		t.Errorf("expected system type entry to be a plain include flag, got nested=%v flag=%v", sysEntry.IsNested(), sysEntry.Flag())
	}
	if f.Sprints == nil || len(f.Sprints.Items) != 1 {
		t.Errorf("Sprints.Items: want 1, got %v", f.Sprints)
	}
	sprintEntry, ok := f.Sprints.Items[sprintID.String()]
	if !ok || !sprintEntry.Flag() {
		t.Errorf("expected sprint entry to be included, got %+v", sprintEntry)
	}
}

// TestCreateViewRequest_ToCreateInput_PluginConfigRoundtrip verifies plugin
// fields are copied from DTO config to domain config through toViewConfig.
func TestCreateViewRequest_ToCreateInput_PluginConfigRoundtrip(t *testing.T) {
	sprintID := uuid.New()
	projectID := uuid.New()
	pluginID := uuid.NewString()

	req := dto.CreateViewRequest{
		Name:     "Plugin View",
		ViewType: sprintdom.ViewTypePlugin,
		Config: &dto.ViewConfigDTO{
			PluginManifestID: pluginID,
			PluginComponent:  "KanbanByPriority",
		},
	}

	input := req.ToCreateInput(sprintID, projectID)

	if input.Config.PluginID != pluginID {
		t.Errorf("PluginID: want %q, got %q", pluginID, input.Config.PluginID)
	}
	if input.Config.PluginComponent != "KanbanByPriority" {
		t.Errorf("PluginComponent: want %q, got %q", "KanbanByPriority", input.Config.PluginComponent)
	}
}

// TestCreateViewRequest_ToCreateInput_PageSizeRoundtrip verifies the
// page_size field is copied from DTO config to domain config through
// toViewConfig.
func TestCreateViewRequest_ToCreateInput_PageSizeRoundtrip(t *testing.T) {
	sprintID := uuid.New()
	projectID := uuid.New()

	req := dto.CreateViewRequest{
		Name:     "Table",
		ViewType: sprintdom.ViewTypeTable,
		Config: &dto.ViewConfigDTO{
			PageSize: 100,
		},
	}

	input := req.ToCreateInput(sprintID, projectID)

	if input.Config.PageSize != 100 {
		t.Errorf("PageSize: want 100, got %d", input.Config.PageSize)
	}
}

// TestCreateViewRequest_ToCreateInput_InitialPageSizeRoundtrip verifies the
// initial_page_size field is copied from DTO config to domain config through
// toViewConfig, independently of PageSize.
func TestCreateViewRequest_ToCreateInput_InitialPageSizeRoundtrip(t *testing.T) {
	sprintID := uuid.New()
	projectID := uuid.New()

	req := dto.CreateViewRequest{
		Name:     "Board",
		ViewType: sprintdom.ViewTypeBoard,
		Config: &dto.ViewConfigDTO{
			PageSize:        20,
			InitialPageSize: 10,
		},
	}

	input := req.ToCreateInput(sprintID, projectID)

	if input.Config.InitialPageSize != 10 {
		t.Errorf("InitialPageSize: want 10, got %d", input.Config.InitialPageSize)
	}
	if input.Config.PageSize != 20 {
		t.Errorf("PageSize: want 20, got %d", input.Config.PageSize)
	}
}

// TestViewFiltersDTO_NilTaskTypesPreserved verifies that a nil TaskTypes does
// not get set to a non-nil value after a roundtrip through entity → DTO.
func TestViewFiltersDTO_NilTaskTypesPreserved(t *testing.T) {
	entity := &sprintdom.SprintView{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Table",
		ViewType:  sprintdom.ViewTypeTable,
		Config: sprintdom.ViewConfig{
			Filters: &sprintdom.ViewFilters{
				Statuses: &sprintdom.FilterConfig{
					All: false,
					Items: map[string]sprintdom.FilterEntry{
						uuid.NewString(): sprintdom.FilterEntryInclude(),
					},
				},
				// TaskTypes intentionally absent.
			},
		},
	}

	resp := dto.ViewFromEntity(entity)
	if resp.Config.Filters == nil {
		t.Fatal("expected non-nil Filters")
	}
	if resp.Config.Filters.TaskTypes != nil {
		t.Errorf("expected nil TaskTypes, got %+v", resp.Config.Filters.TaskTypes)
	}
}
