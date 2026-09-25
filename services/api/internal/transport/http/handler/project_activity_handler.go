package handler

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	activitydom "github.com/Paca-AI/api/internal/domain/activity"
	attachmentdom "github.com/Paca-AI/api/internal/domain/attachment"
	"github.com/Paca-AI/api/internal/events"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// activityLister reads the activity log (activitysvc.Service).
type activityLister interface {
	List(ctx context.Context, f activitydom.ListFilter, limit int) ([]*activitydom.Activity, bool, error)
}

// ProjectActivityHandler serves the project-wide activity log.
type ProjectActivityHandler struct {
	svc       activityLister
	avatarSvc attachmentdom.AvatarService
}

// NewProjectActivityHandler returns a handler over svc. avatarSvc may be nil,
// in which case actor avatar URLs are omitted.
func NewProjectActivityHandler(svc activityLister, avatarSvc attachmentdom.AvatarService) *ProjectActivityHandler {
	return &ProjectActivityHandler{svc: svc, avatarSvc: avatarSvc}
}

var projectActivityEntityTypes = []string{
	string(events.EntityTask), string(events.EntityDoc), string(events.EntitySprint),
	string(events.EntityView), string(events.EntityAutomation), string(events.EntityEnvironment),
	string(events.EntityMember), string(events.EntityProject), string(events.EntityRole),
	string(events.EntityAgent),
}

var projectActivityOrigins = []string{
	string(events.OriginUser), string(events.OriginAgent), string(events.OriginAutomation),
	string(events.OriginJev), string(events.OriginAnnotation), string(events.OriginSystem),
}

// ListActivities handles GET /projects/:projectId/activities.
//
// Supported query params (all optional, combine with AND; comma lists OR
// within themselves):
//   - entity_type=<task,doc,sprint,view,automation,environment,member,project,role,agent>
//   - actor_id=<project member id,...>
//   - origin=<user,agent,automation,jev,annotation,system>
//   - activity_type=<topic,...>             e.g. task.created,sprint.completed
//   - created_after=<YYYY-MM-DD|RFC3339>    on/after this date/instant
//   - created_before=<YYYY-MM-DD|RFC3339>   before this date/instant
//   - search=<text>                         entity title or activity content
//   - cursor=<opaque>, page_size=<1-200>    keyset pagination (see next_cursor)
func (h *ProjectActivityHandler) ListActivities(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	pageSize, err := parsePageSize(r, 50, 200)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	f, err := parseActivityFilter(r, "entity_type", projectActivityEntityTypes)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	f.ProjectID = projectID

	q := r.URL.Query()
	if f.Origins, err = parseEnumList(q.Get("origin"), projectActivityOrigins, "origin"); err != nil {
		presenter.Error(w, r, err)
		return
	}
	f.ActivityTypes = splitCommaList(q.Get("activity_type"))
	for _, raw := range splitCommaList(q.Get("actor_id")) {
		id, err := uuid.Parse(raw)
		if err != nil {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid actor_id: "+raw))
			return
		}
		f.ActorMemberIDs = append(f.ActorMemberIDs, id)
	}

	items, hasMore, err := h.svc.List(r.Context(), f, pageSize)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.ProjectActivityResponse, 0, len(items))
	for _, a := range items {
		item := dto.ProjectActivityFromEntity(a)
		if h.avatarSvc != nil {
			item.ActorAvatarURL, _ = h.avatarSvc.ResolveAvatarURL(r.Context(), a.ActorAvatarKey)
			item.ActorAvatarThumbURL, _ = h.avatarSvc.ResolveAvatarURL(r.Context(), a.ActorAvatarThumbKey)
		}
		resp = append(resp, item)
	}
	presenter.OK(w, r, map[string]any{
		"items":       resp,
		"page_size":   pageSize,
		"next_cursor": nextActivityCursor(items, hasMore),
	})
}

// parseActivityFilter reads the query params every activity list shares:
// the entity type list (under typeParam, limited to validTypes),
// created_after/created_before, search and cursor.
func parseActivityFilter(r *http.Request, typeParam string, validTypes []string) (activitydom.ListFilter, error) {
	q := r.URL.Query()
	var f activitydom.ListFilter
	var err error
	if f.EntityTypes, err = parseEnumList(q.Get(typeParam), validTypes, typeParam); err != nil {
		return f, err
	}
	if raw := strings.TrimSpace(q.Get("created_after")); raw != "" {
		t, ok := parseCreatedAfterBound(raw)
		if !ok {
			return f, apierr.New(apierr.CodeBadRequest, "invalid created_after")
		}
		f.CreatedAfter = t
	}
	if raw := strings.TrimSpace(q.Get("created_before")); raw != "" {
		t, ok := parseCreatedBeforeBound(raw)
		if !ok {
			return f, apierr.New(apierr.CodeBadRequest, "invalid created_before")
		}
		f.CreatedBefore = t
	}
	f.Search = strings.TrimSpace(q.Get("search"))
	if raw := q.Get("cursor"); raw != "" {
		c, err := activitydom.DecodeCursor(raw)
		if err != nil {
			return f, apierr.New(apierr.CodeBadRequest, "invalid cursor")
		}
		f.Cursor = c
	}
	return f, nil
}

// nextActivityCursor is the cursor resuming after a page, or nil when it was
// the last one.
func nextActivityCursor(items []*activitydom.Activity, hasMore bool) *string {
	if !hasMore || len(items) == 0 {
		return nil
	}
	s := activitydom.EncodeCursor(items[len(items)-1])
	return &s
}

// parseEnumList splits a comma list and rejects any value outside valid.
func parseEnumList(raw string, valid []string, param string) ([]string, error) {
	vals := splitCommaList(raw)
	for _, v := range vals {
		if !slices.Contains(valid, v) {
			return nil, apierr.New(apierr.CodeBadRequest, "invalid "+param+": "+v)
		}
	}
	return vals, nil
}
