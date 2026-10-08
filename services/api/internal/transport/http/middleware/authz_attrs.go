package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	projectdom "github.com/Paca-AI/api/internal/domain/project"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// maxAttrPeekBody bounds the body read to learn which attributes a request
// sets. It is far above any real task or document: documents carry their whole
// content in the body.
const maxAttrPeekBody = 64 << 20

// AttrExtractor returns the attribute values (keyed like "task.sprint_id") a
// request body sets. A field absent from the body must be absent from the
// result; a field explicitly set to null maps to a nil slice (attribute
// cleared). It must not fail on a body it does not understand: the handler
// answers that with its own 400.
type AttrExtractor func(ctx context.Context, body map[string]json.RawMessage) (map[string][]string, error)

// RequireRequestAttrs authorizes the attribute values a request sets, on top of
// the gate that already allowed the action on the resource as it stands.
//
// With create, res names the kind's collection and the new resource is
// authorized with the request's attributes (iam.AuthorizeCreate); otherwise res
// names the entity and, when the body changes attributes, both its current
// and its new values must be allowed (iam.AuthorizeChange), so a task cannot be
// moved out of, or into, a place the caller has no access to. An update that
// sets no attribute adds nothing here. Declare it after the route's action gate.
func RequireRequestAttrs(a *iam.Authorizer, action iam.Action, res ResourceResolver, extract AttrExtractor, create bool) func(http.Handler) http.Handler {
	return RequireRequestAttrsFromRequest(a, action, res, func(r *http.Request, body map[string]json.RawMessage) (map[string][]string, error) {
		return extract(r.Context(), body)
	}, create)
}

// RequestAttrExtractor is an AttrExtractor that also sees the request, for
// attributes carried by the URL (query string) rather than the body. The same
// rules apply: absent means absent, and it must not fail on a body it does
// not understand.
type RequestAttrExtractor func(r *http.Request, body map[string]json.RawMessage) (map[string][]string, error)

// RequireRequestAttrsFromRequest is RequireRequestAttrs for a RequestAttrExtractor.
func RequireRequestAttrsFromRequest(a *iam.Authorizer, action iam.Action, res ResourceResolver, extract RequestAttrExtractor, create bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := IAMPrincipalFrom(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			resource, err := res(r)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			buf, err := peekBodyLimit(r, maxAttrPeekBody)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			var body map[string]json.RawMessage
			if json.Unmarshal(buf, &body) != nil {
				body = nil // not a JSON object: the handler's 400, no attributes set
			}
			attrs, err := extract(r, body)
			if err != nil {
				presenter.Error(w, r, err)
				return
			}
			if a == nil {
				presenter.Error(w, r, apierr.New(apierr.CodeInternalError, "authorization not configured"))
				return
			}
			var res iam.Result
			switch {
			case create:
				res, err = a.AuthorizeCreate(r.Context(), p, string(action), resource, attrs)
			case len(attrs) == 0:
				next.ServeHTTP(w, r)
				return
			default:
				res, err = a.AuthorizeChange(r.Context(), p, string(action), resource, attrs)
			}
			if proceedIfAllowed(w, r, res.Allowed, err) {
				next.ServeHTTP(w, r)
			}
		})
	}
}

// peekBodyLimit is peekBody with a caller-chosen limit.
func peekBodyLimit(r *http.Request, limit int64) ([]byte, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "could not read request body")
	}
	if int64(len(buf)) > limit {
		return nil, apierr.New(apierr.CodeBadRequest, "request body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))
	return buf, nil
}

// uuidField reads the UUID field of a body into attrs[key]: absent leaves attrs
// alone, null clears the attribute, a UUID sets it. Anything else is a 400, as
// the handler would answer it.
func uuidField(body map[string]json.RawMessage, field, key string, attrs map[string][]string) error {
	raw, ok := body[field]
	if !ok {
		return nil
	}
	if string(bytes.TrimSpace(raw)) == "null" {
		attrs[key] = nil
		return nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return apierr.New(apierr.CodeBadRequest, "invalid "+field)
	}
	attrs[key] = []string{id.String()}
	return nil
}

// TaskAttrs extracts the attributes a task create or update body sets:
// task.sprint_id, task.status_id, task.type_id and task.assignee_id. The
// request names assignees by project member id; members resolves them to the
// principal ids policies compare against (principal.id).
func TaskAttrs(members MemberPrincipalLookup) AttrExtractor {
	return func(ctx context.Context, body map[string]json.RawMessage) (map[string][]string, error) {
		attrs := map[string][]string{}
		for field, key := range map[string]string{"sprint_id": "task.sprint_id", "status_id": "task.status_id", "task_type_id": "task.type_id"} {
			if err := uuidField(body, field, key, attrs); err != nil {
				return nil, err
			}
		}
		raw, ok := body["assignee_ids"]
		if !ok {
			return attrs, nil
		}
		var memberIDs []uuid.UUID
		if string(bytes.TrimSpace(raw)) != "null" {
			if err := json.Unmarshal(raw, &memberIDs); err != nil {
				return nil, apierr.New(apierr.CodeBadRequest, "invalid assignee_ids")
			}
		}
		principals := []string{}
		for _, id := range memberIDs {
			if members == nil {
				return nil, apierr.New(apierr.CodeInternalError, "authorization not configured")
			}
			pid, err := members.MemberPrincipalID(ctx, id)
			if err != nil {
				return nil, err
			}
			principals = append(principals, pid)
		}
		attrs["task.assignee_id"] = principals
		return attrs, nil
	}
}

// DocAttrs extracts the attribute a document create or update body sets:
// doc.folder_id (the authorizer derives doc.ancestor_folder_ids from it).
func DocAttrs(_ context.Context, body map[string]json.RawMessage) (map[string][]string, error) {
	attrs := map[string][]string{}
	if err := uuidField(body, "folder_id", "doc.folder_id", attrs); err != nil {
		return nil, err
	}
	return attrs, nil
}

// MemberRepoLookup implements MemberPrincipalLookup over the member repository.
type MemberRepoLookup struct{ Repo projectdom.MemberRepository }

// MemberPrincipalID implements MemberPrincipalLookup.
func (l MemberRepoLookup) MemberPrincipalID(ctx context.Context, memberID uuid.UUID) (string, error) {
	m, err := l.Repo.FindMemberByID(ctx, memberID)
	if err != nil {
		return "", err
	}
	if m.AgentID != nil {
		return m.AgentID.String(), nil
	}
	return m.UserID.String(), nil
}

// MemberPrincipalLookup maps a project member id to the id of the user or
// agent behind it (what a policy's principal.id and task.assignee_id hold). An
// unknown member is an error: the request names someone who does not exist, and
// the handler would reject it too.
type MemberPrincipalLookup interface {
	MemberPrincipalID(ctx context.Context, memberID uuid.UUID) (string, error)
}

// ViewAttrs extracts the attribute a view create request sets: view.sprint_id.
// The sprint is not in the body: it is the `sprint_id` query parameter of a
// `context=sprint` (the default) request. Backlog and timeline views are
// project-level and have no sprint, so the attribute is cleared (absent). A
// missing or malformed sprint id, or an unknown context, is also treated as
// "no sprint": the request is authorized as sprintless (which a positive
// sprint condition refuses) and the handler answers its own 400.
func ViewAttrs(r *http.Request, _ map[string]json.RawMessage) (map[string][]string, error) {
	if c := r.URL.Query().Get("context"); c == "" || c == "sprint" {
		if id, err := uuid.Parse(r.URL.Query().Get("sprint_id")); err == nil {
			return map[string][]string{"view.sprint_id": {id.String()}}, nil
		}
	}
	return map[string][]string{"view.sprint_id": nil}, nil
}
