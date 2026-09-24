// Package sprintsvc implements sprint view application services.
package sprintsvc

import (
	"context"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	sprintdom "github.com/Paca-AI/api/internal/domain/sprint"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/platform/messaging"
)

// ViewService is the concrete implementation of sprintdom.ViewService.
type ViewService struct {
	repo       sprintdom.ViewRepository
	sprintRepo sprintdom.SprintRepository
	taskRepo   taskdom.TaskRepository
	publisher  *messaging.Publisher
}

// NewViewService returns a configured ViewService. sprintRepo and taskRepo
// are used to verify that a sprint-context view's sprint, and a task
// position's task, belong to the project the caller was authorized against.
// publisher may be nil; real-time events are then skipped silently.
func NewViewService(repo sprintdom.ViewRepository, sprintRepo sprintdom.SprintRepository, taskRepo taskdom.TaskRepository, publisher *messaging.Publisher) *ViewService {
	return &ViewService{repo: repo, sprintRepo: sprintRepo, taskRepo: taskRepo, publisher: publisher}
}

// sprintInProject returns nil when sprintID resolves to a sprint that
// belongs to projectID, and sprintdom.ErrSprintNotFound otherwise (including
// when the sprint does not exist at all).
func (s *ViewService) sprintInProject(ctx context.Context, projectID, sprintID uuid.UUID) error {
	sp, err := s.sprintRepo.FindSprintByID(ctx, sprintID)
	if err != nil {
		return err
	}
	if sp.ProjectID != projectID {
		return sprintdom.ErrSprintNotFound
	}
	return nil
}

// publish sends a real-time pub/sub notification for a view change. Errors
// are silently swallowed so a messaging failure never blocks the primary
// HTTP response — this is how the frontend learns to refresh its sprint/
// backlog/timeline view list instead of relying on query staleTime.
func (s *ViewService) publish(ctx context.Context, topic string, payload map[string]any) {
	if s.publisher == nil {
		return
	}
	_ = s.publisher.Publish(ctx, events.ChannelRealtime, map[string]any{
		"type":    topic,
		"payload": payload,
	})
}

// viewPayload builds the common event payload fields shared by all view
// events: project_id, view_id, view_context, and — for sprint-context views
// — sprint_id.
func viewPayload(v *sprintdom.SprintView) map[string]any {
	payload := map[string]any{
		"project_id":   v.ProjectID.String(),
		"view_id":      v.ID.String(),
		"view_context": string(v.ViewContext),
	}
	if v.SprintID != nil {
		payload["sprint_id"] = v.SprintID.String()
	}
	return payload
}

// hasPluginConfig reports whether cfg carries the plugin binding required for
// a "plugin" view_type. Non-plugin view types are always valid here — this
// only guards against a "plugin" view being persisted without the
// PluginID/PluginComponent pair the frontend needs to resolve its extension
// point (see apps/web's InteractionLayout, which falls back to a "Plugin not
// available" empty state when either is missing or doesn't match a
// registered plugin). Enforced server-side so no caller — MCP client, stale
// tool version, or otherwise — can persist a broken plugin view even when it
// skips (or predates) the equivalent client-side check.
func hasPluginConfig(vt sprintdom.ViewType, cfg *sprintdom.ViewConfig) bool {
	if vt != sprintdom.ViewTypePlugin {
		return true
	}
	if cfg == nil {
		return false
	}
	return strings.TrimSpace(cfg.PluginID) != "" && strings.TrimSpace(cfg.PluginComponent) != ""
}

// ListViews returns all views for a sprint, verifying the sprint belongs to projectID.
func (s *ViewService) ListViews(ctx context.Context, projectID, sprintID uuid.UUID) ([]*sprintdom.SprintView, error) {
	if err := s.sprintInProject(ctx, projectID, sprintID); err != nil {
		return nil, err
	}
	return s.repo.ListViews(ctx, sprintID)
}

// ListProjectViews returns all views for a project filtered by viewCtx.
func (s *ViewService) ListProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext) ([]*sprintdom.SprintView, error) {
	return s.repo.ListProjectViews(ctx, projectID, viewCtx)
}

// GetView returns the view with the given ID, verifying it belongs to projectID.
func (s *ViewService) GetView(ctx context.Context, projectID, id uuid.UUID) (*sprintdom.SprintView, error) {
	v, err := s.repo.FindViewByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.ProjectID != projectID {
		return nil, sprintdom.ErrViewNotFound
	}
	return v, nil
}

// CreateView creates a new view for the given sprint. When in.SprintID is
// set (sprint-context view), verifies the sprint belongs to in.ProjectID.
func (s *ViewService) CreateView(ctx context.Context, in sprintdom.CreateViewInput) (*sprintdom.SprintView, error) {
	if in.SprintID != nil {
		if err := s.sprintInProject(ctx, in.ProjectID, *in.SprintID); err != nil {
			return nil, err
		}
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, sprintdom.ErrViewNameInvalid
	}

	vt := in.ViewType
	if vt == "" {
		vt = sprintdom.ViewTypeTable
	}
	if !sprintdom.ValidViewTypes[vt] {
		return nil, sprintdom.ErrViewTypeInvalid
	}
	if !hasPluginConfig(vt, &in.Config) {
		return nil, sprintdom.ErrViewPluginConfigRequired
	}

	now := time.Now()
	v := &sprintdom.SprintView{
		ID:          uuid.New(),
		SprintID:    in.SprintID,
		ProjectID:   in.ProjectID,
		Name:        name,
		ViewType:    vt,
		Config:      in.Config,
		Position:    in.Position,
		ViewContext: in.ViewContext,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateView(ctx, v); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicViewCreated, viewPayload(v))
	return v, nil
}

// UpdateView updates the mutable fields of an existing view,
// verifying it belongs to projectID.
func (s *ViewService) UpdateView(ctx context.Context, projectID, id uuid.UUID, in sprintdom.UpdateViewInput) (*sprintdom.SprintView, error) {
	v, err := s.repo.FindViewByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if v.ProjectID != projectID {
		return nil, sprintdom.ErrViewNotFound
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return nil, sprintdom.ErrViewNameInvalid
		}
		v.Name = name
	}
	if in.ViewType != nil {
		if !sprintdom.ValidViewTypes[*in.ViewType] {
			return nil, sprintdom.ErrViewTypeInvalid
		}
		v.ViewType = *in.ViewType
	}
	if in.Config != nil {
		v.Config = *in.Config
	}
	if in.Position != nil {
		v.Position = *in.Position
	}
	if !hasPluginConfig(v.ViewType, &v.Config) {
		return nil, sprintdom.ErrViewPluginConfigRequired
	}
	v.UpdatedAt = time.Now()

	if err := s.repo.UpdateView(ctx, v); err != nil {
		return nil, err
	}
	s.publish(ctx, events.TopicViewUpdated, viewPayload(v))
	return v, nil
}

// DeleteView removes a view by ID.  Deletion of the last remaining view is
// rejected with ErrViewIsLastView.  Verifies the view belongs to projectID.
func (s *ViewService) DeleteView(ctx context.Context, projectID, id uuid.UUID) error {
	v, err := s.repo.FindViewByID(ctx, id)
	if err != nil {
		return err
	}
	if v.ProjectID != projectID {
		return sprintdom.ErrViewNotFound
	}

	var count int
	if v.SprintID != nil {
		count, err = s.repo.CountViews(ctx, *v.SprintID)
	} else {
		count, err = s.repo.CountProjectViews(ctx, v.ProjectID, v.ViewContext)
	}
	if err != nil {
		return err
	}
	if count <= 1 {
		return sprintdom.ErrViewIsLastView
	}

	if err := s.repo.DeleteView(ctx, id); err != nil {
		return err
	}
	s.publish(ctx, events.TopicViewDeleted, viewPayload(v))
	return nil
}

// MoveTask updates the manual position of a task within a view,
// verifying the view and the task both belong to projectID.
func (s *ViewService) MoveTask(ctx context.Context, projectID, viewID uuid.UUID, in sprintdom.MoveTaskInput) error {
	v, err := s.repo.FindViewByID(ctx, viewID)
	if err != nil {
		return err
	}
	if v.ProjectID != projectID {
		return sprintdom.ErrViewNotFound
	}
	t, err := s.taskRepo.FindTaskByID(ctx, in.TaskID)
	if err != nil {
		return err
	}
	if t.ProjectID != projectID {
		return taskdom.ErrTaskNotFound
	}
	pos := &sprintdom.ViewTaskPosition{
		ID:       uuid.New(),
		ViewID:   viewID,
		TaskID:   in.TaskID,
		Position: in.Position,
		GroupKey: in.GroupKey,
	}
	if err := s.repo.UpsertTaskPosition(ctx, pos); err != nil {
		return err
	}
	s.publish(ctx, events.TopicViewTaskMoved, map[string]any{
		"project_id": projectID.String(),
		"view_id":    viewID.String(),
	})
	return nil
}

// BulkMoveTasks updates the manual positions of multiple tasks within a view
// in a single database round-trip.  Verifies the view and every task belong
// to projectID.
func (s *ViewService) BulkMoveTasks(ctx context.Context, projectID, viewID uuid.UUID, items []sprintdom.MoveTaskInput) error {
	v, err := s.repo.FindViewByID(ctx, viewID)
	if err != nil {
		return err
	}
	if v.ProjectID != projectID {
		return sprintdom.ErrViewNotFound
	}
	for _, in := range items {
		t, err := s.taskRepo.FindTaskByID(ctx, in.TaskID)
		if err != nil {
			return err
		}
		if t.ProjectID != projectID {
			return taskdom.ErrTaskNotFound
		}
	}
	positions := make([]*sprintdom.ViewTaskPosition, 0, len(items))
	for _, in := range items {
		positions = append(positions, &sprintdom.ViewTaskPosition{
			ID:       uuid.New(),
			ViewID:   viewID,
			TaskID:   in.TaskID,
			Position: in.Position,
			GroupKey: in.GroupKey,
		})
	}
	if err := s.repo.BulkUpsertTaskPositions(ctx, positions); err != nil {
		return err
	}
	s.publish(ctx, events.TopicViewTaskMoved, map[string]any{
		"project_id": projectID.String(),
		"view_id":    viewID.String(),
	})
	return nil
}

// ListTaskPositions returns the manual ordering for all tasks in a view,
// verifying the view belongs to projectID.
func (s *ViewService) ListTaskPositions(ctx context.Context, projectID, viewID uuid.UUID) ([]*sprintdom.ViewTaskPosition, error) {
	v, err := s.repo.FindViewByID(ctx, viewID)
	if err != nil {
		return nil, err
	}
	if v.ProjectID != projectID {
		return nil, sprintdom.ErrViewNotFound
	}
	return s.repo.ListTaskPositions(ctx, viewID)
}

// ReorderViews reorders all views belonging to a sprint, verifying the
// sprint belongs to projectID.  viewIDs must contain exactly the IDs of all
// views for that sprint in the desired display order.
func (s *ViewService) ReorderViews(ctx context.Context, projectID, sprintID uuid.UUID, viewIDs []uuid.UUID) error {
	if err := s.sprintInProject(ctx, projectID, sprintID); err != nil {
		return err
	}
	existing, err := s.repo.ListViews(ctx, sprintID)
	if err != nil {
		return err
	}
	if err := s.validateAndReorder(ctx, existing, viewIDs); err != nil {
		return err
	}
	if len(existing) > 0 {
		payload := map[string]any{
			"project_id":   existing[0].ProjectID.String(),
			"view_context": string(existing[0].ViewContext),
			"sprint_id":    sprintID.String(),
		}
		s.publish(ctx, events.TopicViewReordered, payload)
	}
	return nil
}

// ReorderProjectViews reorders all views for a project+context.
// viewIDs must contain exactly the IDs of all views for that project+context in the desired order.
func (s *ViewService) ReorderProjectViews(ctx context.Context, projectID uuid.UUID, viewCtx sprintdom.ViewContext, viewIDs []uuid.UUID) error {
	existing, err := s.repo.ListProjectViews(ctx, projectID, viewCtx)
	if err != nil {
		return err
	}
	if err := s.validateAndReorder(ctx, existing, viewIDs); err != nil {
		return err
	}
	if len(existing) > 0 {
		s.publish(ctx, events.TopicViewReordered, map[string]any{
			"project_id":   projectID.String(),
			"view_context": string(viewCtx),
		})
	}
	return nil
}

// SetUserViewConfig stores the current user's personal config for a view,
// verifying it belongs to projectID, and returns the view carrying the
// effective (merged) config the caller just set. The write is private to the
// user: the shared sprint_views row is untouched and no project-wide
// real-time event is published, so other members are unaffected.
//
// cfg is diffed against the *current* shared row and only the fields that
// actually differ are persisted as the override (see diffViewConfig) — a
// field the user leaves matching the shared value keeps tracking that shared
// value if it changes later, instead of freezing at today's snapshot. When
// every field matches shared (the diff is empty), any existing override row
// is removed rather than upserting a no-op, so HasPersonalConfig stays
// accurate and user_view_configs doesn't accumulate empty rows.
//
// Known race (accepted, not fixed): cfg reflects whatever the caller's UI
// last fetched the shared config as, which can be stale if an admin changes
// the shared default while the caller's settings panel is still open. A
// field the user never touched could then be re-diffed against a shared
// value that has since moved, and get spuriously captured in the override.
// This is narrow (requires a concurrent shared-config edit mid-edit) and
// self-healing (ClearUserViewConfig/"use team default" recovers it in one
// click, which didn't exist before this override model).
func (s *ViewService) SetUserViewConfig(ctx context.Context, projectID, viewID, userID uuid.UUID, cfg sprintdom.ViewConfig) (*sprintdom.SprintView, error) {
	v, err := s.repo.FindViewByID(ctx, viewID)
	if err != nil {
		return nil, err
	}
	if v.ProjectID != projectID {
		return nil, sprintdom.ErrViewNotFound
	}
	// A plugin view still needs its plugin binding in the effective config.
	if !hasPluginConfig(v.ViewType, &cfg) {
		return nil, sprintdom.ErrViewPluginConfigRequired
	}
	sparse := diffViewConfig(v.Config, cfg)
	sharedConfig := v.Config
	if isZeroViewConfig(sparse) {
		if err := s.repo.DeleteUserViewConfig(ctx, viewID, userID); err != nil {
			return nil, err
		}
		v.HasPersonalConfig = false
	} else {
		if err := s.repo.UpsertUserViewConfig(ctx, viewID, userID, sparse); err != nil {
			return nil, err
		}
		v.HasPersonalConfig = true
		v.SharedConfig = sharedConfig
	}
	v.Config = cfg
	return v, nil
}

// ClearUserViewConfig removes the current user's personal override for a
// view, verifying it belongs to projectID, and returns the view now carrying
// the shared default (HasPersonalConfig is false). Deleting a nonexistent
// override is a harmless no-op, matching UpsertUserViewConfig's idempotence.
func (s *ViewService) ClearUserViewConfig(ctx context.Context, projectID, viewID, userID uuid.UUID) (*sprintdom.SprintView, error) {
	v, err := s.repo.FindViewByID(ctx, viewID)
	if err != nil {
		return nil, err
	}
	if v.ProjectID != projectID {
		return nil, sprintdom.ErrViewNotFound
	}
	if err := s.repo.DeleteUserViewConfig(ctx, viewID, userID); err != nil {
		return nil, err
	}
	return v, nil
}

// OverlayUserConfigs merges each view's Config with the user's personal
// override where one exists (see mergeViewConfig) and sets HasPersonalConfig
// accordingly, leaving views without an override untouched. A nil user or
// empty view list is a no-op. The passed views are mutated in place; callers
// pass per-request copies (cache hits deserialize fresh objects), so the shared
// cache is never affected.
func (s *ViewService) OverlayUserConfigs(ctx context.Context, userID uuid.UUID, views []*sprintdom.SprintView) error {
	if userID == uuid.Nil || len(views) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(views))
	for _, v := range views {
		if v != nil {
			ids = append(ids, v.ID)
		}
	}
	overrides, err := s.repo.GetUserViewConfigs(ctx, userID, ids)
	if err != nil {
		return err
	}
	if len(overrides) == 0 {
		return nil
	}
	for _, v := range views {
		if v == nil {
			continue
		}
		cfg, ok := overrides[v.ID]
		v.HasPersonalConfig = ok
		if ok {
			v.SharedConfig = v.Config
			v.Config = mergeViewConfig(v.SharedConfig, cfg)
		}
	}
	return nil
}

// mergeViewConfig returns the effective config for a view: each field of
// override wins when the user has personally set it; shared's value is used
// otherwise. PluginID/PluginComponent always come from shared — plugin
// binding is structural, never a personal preference (see hasPluginConfig).
//
// Known limitation: Fields and CollapsedColumns, like the plain string/int
// fields below, cannot distinguish "the user explicitly chose zero items"
// from "never touched" — both collapse to Go's zero value. This predates
// this merge logic (the single-layer "empty means default" convention
// already had the same ambiguity for a single config) and isn't made worse
// by it; fully closing it would require changing every field to a
// pointer/presence-tracked type, a much larger wire-format change not
// justified for the win it buys.
func mergeViewConfig(shared, override sprintdom.ViewConfig) sprintdom.ViewConfig {
	out := shared
	if len(override.Fields) > 0 {
		out.Fields = override.Fields
	}
	if override.ColumnBy != "" {
		out.ColumnBy = override.ColumnBy
	}
	if override.Swimlanes != "" {
		out.Swimlanes = override.Swimlanes
	}
	if override.SortBy != "" {
		out.SortBy = override.SortBy
	}
	if override.FieldSum != "" {
		out.FieldSum = override.FieldSum
	}
	if override.SliceBy != "" {
		out.SliceBy = override.SliceBy
	}
	if override.Filters != nil {
		out.Filters = override.Filters
	}
	if len(override.CollapsedColumns) > 0 {
		out.CollapsedColumns = override.CollapsedColumns
	}
	if override.PageSize != 0 {
		out.PageSize = override.PageSize
	}
	if override.InitialPageSize != 0 {
		out.InitialPageSize = override.InitialPageSize
	}
	// out.PluginID / out.PluginComponent intentionally left at shared's value.
	return out
}

// diffViewConfig returns the sparse subset of desired that differs from
// shared, suitable for persisting as a personal override: fields identical
// to shared are left at Go zero value so a later mergeViewConfig falls back
// to whatever shared holds at read time — even if shared changes afterward.
// PluginID/PluginComponent are never included (see mergeViewConfig).
func diffViewConfig(shared, desired sprintdom.ViewConfig) sprintdom.ViewConfig {
	var out sprintdom.ViewConfig
	if !reflect.DeepEqual(desired.Fields, shared.Fields) {
		out.Fields = desired.Fields
	}
	if desired.ColumnBy != shared.ColumnBy {
		out.ColumnBy = desired.ColumnBy
	}
	if desired.Swimlanes != shared.Swimlanes {
		out.Swimlanes = desired.Swimlanes
	}
	if desired.SortBy != shared.SortBy {
		out.SortBy = desired.SortBy
	}
	if desired.FieldSum != shared.FieldSum {
		out.FieldSum = desired.FieldSum
	}
	if desired.SliceBy != shared.SliceBy {
		out.SliceBy = desired.SliceBy
	}
	if !reflect.DeepEqual(desired.Filters, shared.Filters) {
		out.Filters = desired.Filters
	}
	if !reflect.DeepEqual(desired.CollapsedColumns, shared.CollapsedColumns) {
		out.CollapsedColumns = desired.CollapsedColumns
	}
	if desired.PageSize != shared.PageSize {
		out.PageSize = desired.PageSize
	}
	if desired.InitialPageSize != shared.InitialPageSize {
		out.InitialPageSize = desired.InitialPageSize
	}
	return out
}

// isZeroViewConfig reports whether cfg has no fields set — i.e. an override
// that would make no difference once merged with any shared config.
func isZeroViewConfig(cfg sprintdom.ViewConfig) bool {
	return reflect.DeepEqual(cfg, sprintdom.ViewConfig{})
}

// validateAndReorder checks that viewIDs exactly matches the IDs of existing
// views (same count, no unknowns) then persists the new positions.
func (s *ViewService) validateAndReorder(ctx context.Context, existing []*sprintdom.SprintView, viewIDs []uuid.UUID) error {
	if len(viewIDs) != len(existing) {
		return sprintdom.ErrViewReorderInvalid
	}
	existingSet := make(map[uuid.UUID]struct{}, len(existing))
	for _, v := range existing {
		existingSet[v.ID] = struct{}{}
	}
	items := make([]sprintdom.ViewReorderItem, 0, len(viewIDs))
	for i, id := range viewIDs {
		if _, ok := existingSet[id]; !ok {
			return sprintdom.ErrViewReorderInvalid
		}
		items = append(items, sprintdom.ViewReorderItem{ID: id, Position: float64(i)})
	}
	return s.repo.ReorderViews(ctx, items)
}
