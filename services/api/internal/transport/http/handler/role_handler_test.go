package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/transport/http/handler"
)

// fakeRoleSvc records what the handler passes and returns canned results.
type fakeRoleSvc struct {
	role  *roledom.Role
	roles []*roledom.Role
	err   error

	gotProject   *uuid.UUID
	gotID        uuid.UUID
	gotIn        roledom.RoleInput
	gotRoleIDs   []uuid.UUID
	gotCreatedBy *uuid.UUID
	gotMember    uuid.UUID
	gotSim       roledom.SimulateInput
	issues       []roledom.Issue
	sim          *roledom.SimulationResult
}

func (f *fakeRoleSvc) ListPlatform(context.Context) ([]*roledom.Role, error) { return f.roles, f.err }
func (f *fakeRoleSvc) ListForProject(_ context.Context, p uuid.UUID) ([]*roledom.Role, error) {
	f.gotProject = &p
	return f.roles, f.err
}
func (f *fakeRoleSvc) Get(_ context.Context, p *uuid.UUID, id uuid.UUID) (*roledom.Role, error) {
	f.gotProject, f.gotID = p, id
	return f.role, f.err
}
func (f *fakeRoleSvc) Create(_ context.Context, p *uuid.UUID, in roledom.RoleInput) (*roledom.Role, error) {
	f.gotProject, f.gotIn = p, in
	return f.role, f.err
}
func (f *fakeRoleSvc) Update(_ context.Context, p *uuid.UUID, id uuid.UUID, in roledom.RoleInput) (*roledom.Role, error) {
	f.gotProject, f.gotID, f.gotIn = p, id, in
	return f.role, f.err
}
func (f *fakeRoleSvc) Delete(_ context.Context, p *uuid.UUID, id uuid.UUID) error {
	f.gotProject, f.gotID = p, id
	return f.err
}
func (f *fakeRoleSvc) SetDefault(_ context.Context, id uuid.UUID) (*roledom.Role, error) {
	f.gotID = id
	return f.role, f.err
}
func (f *fakeRoleSvc) ListUserRoles(_ context.Context, id uuid.UUID) ([]*roledom.Role, error) {
	f.gotID = id
	return f.roles, f.err
}
func (f *fakeRoleSvc) ReplaceUserRoles(_ context.Context, id uuid.UUID, ids []uuid.UUID, by *uuid.UUID) ([]*roledom.Role, error) {
	f.gotID, f.gotRoleIDs, f.gotCreatedBy = id, ids, by
	return f.roles, f.err
}
func (f *fakeRoleSvc) ListAgentRoles(_ context.Context, id uuid.UUID) ([]*roledom.Role, error) {
	f.gotID = id
	return f.roles, f.err
}
func (f *fakeRoleSvc) ReplaceAgentRoles(_ context.Context, id uuid.UUID, ids []uuid.UUID, by *uuid.UUID) ([]*roledom.Role, error) {
	f.gotID, f.gotRoleIDs, f.gotCreatedBy = id, ids, by
	return f.roles, f.err
}
func (f *fakeRoleSvc) ListMemberRoles(_ context.Context, p, m uuid.UUID) ([]*roledom.Role, error) {
	f.gotProject, f.gotMember = &p, m
	return f.roles, f.err
}
func (f *fakeRoleSvc) ReplaceMemberRoles(_ context.Context, p, m uuid.UUID, ids []uuid.UUID, by *uuid.UUID) ([]*roledom.Role, error) {
	f.gotProject, f.gotMember, f.gotRoleIDs, f.gotCreatedBy = &p, m, ids, by
	return f.roles, f.err
}
func (f *fakeRoleSvc) Actions() []string { return []string{"tasks:read", "tasks:write"} }
func (f *fakeRoleSvc) AttributeDefs() []roledom.AttributeDef {
	return []roledom.AttributeDef{{Key: "doc.ancestor_folder_ids", ResourceKind: "doc", Type: "string", MultiValued: true, LabelKey: "roles.attributes.doc.ancestor_folder_ids"}}
}
func (f *fakeRoleSvc) ValidatePolicy(json.RawMessage, *uuid.UUID) []roledom.Issue { return f.issues }
func (f *fakeRoleSvc) Simulate(_ context.Context, in roledom.SimulateInput) (*roledom.SimulationResult, error) {
	f.gotSim = in
	return f.sim, f.err
}

func newRoleRouter(svc roledom.Service, sub string) chi.Router {
	h := handler.NewRoleHandler(svc)
	r := chi.NewRouter()
	r.Use(injectAuthClaimsMiddleware(sub))
	r.Get("/admin/roles", h.List)
	r.Post("/admin/roles", h.Create)
	r.Get("/admin/roles/{roleId}", h.Get)
	r.Put("/admin/roles/{roleId}", h.Update)
	r.Delete("/admin/roles/{roleId}", h.Delete)
	r.Put("/admin/roles/{roleId}/default", h.SetDefault)
	r.Get("/admin/users/{userId}/roles", h.ListUserRoles)
	r.Put("/admin/users/{userId}/roles", h.ReplaceUserRoles)
	r.Get("/admin/agents/{agentId}/roles", h.ListAgentRoles)
	r.Put("/admin/agents/{agentId}/roles", h.ReplaceAgentRoles)
	r.Get("/projects/{projectId}/members/{memberId}/roles", h.ListMemberRoles)
	r.Put("/projects/{projectId}/members/{memberId}/roles", h.ReplaceMemberRoles)
	r.Route("/projects/{projectId}/roles", func(r chi.Router) {
		r.Get("/", h.List)
		r.Post("/", h.Create)
		r.Get("/{roleId}", h.Get)
		r.Put("/{roleId}", h.Update)
		r.Delete("/{roleId}", h.Delete)
	})
	r.Get("/roles/actions", h.Actions)
	r.Get("/roles/attribute-schema", h.AttributeSchema)
	r.Post("/roles/validate", h.Validate)
	r.Post("/roles/simulate", h.Simulate)
	return r
}

func doRole(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type roleEnvelope struct {
	Success   bool            `json:"success"`
	Data      json.RawMessage `json:"data"`
	ErrorCode string          `json:"error_code"`
	Issues    []apierr.Issue  `json:"issues"`
}

func decodeRole(t *testing.T, w *httptest.ResponseRecorder) roleEnvelope {
	t.Helper()
	var env roleEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return env
}

const handlerPolicy = `{"version":"2026-10-01","statements":[{"effect":"Allow","actions":["tasks:read"],"resources":["project/*"]}]}`

func sampleRole(project *uuid.UUID) *roledom.Role {
	return &roledom.Role{
		ID: uuid.New(), Name: "Dev", Description: "d", Policy: json.RawMessage(handlerPolicy), ProjectID: project,
		IsSystem: true, IsDefault: true, AttachmentCount: 3,
		CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestRoleHandler_ResponseShape(t *testing.T) {
	p := uuid.New()
	svc := &fakeRoleSvc{role: sampleRole(&p)}
	r := newRoleRouter(svc, uuid.NewString())
	w := doRole(t, r, http.MethodGet, "/admin/roles/"+svc.role.ID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(decodeRole(t, w).Data, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "name", "description", "policy", "project_id", "is_system", "is_default", "attachment_count", "created_at", "updated_at"} {
		if _, ok := got[k]; !ok {
			t.Errorf("response is missing %q: %v", k, got)
		}
	}
	if pol, ok := got["policy"].(map[string]any); !ok || pol["version"] != "2026-10-01" {
		t.Errorf("policy must be a JSON object: %#v", got["policy"])
	}
	if got["project_id"] != p.String() || got["attachment_count"] != float64(3) || got["is_system"] != true || got["is_default"] != true {
		t.Errorf("fields = %v", got)
	}
	if _, legacy := got["permissions"]; legacy {
		t.Error("the response must not carry a legacy permissions map")
	}
	// a platform role renders project_id as null
	svc.role = sampleRole(nil)
	w = doRole(t, r, http.MethodGet, "/admin/roles/"+svc.role.ID.String(), nil)
	got = nil
	_ = json.Unmarshal(decodeRole(t, w).Data, &got)
	if v, ok := got["project_id"]; !ok || v != nil {
		t.Errorf("platform role project_id = %v (present=%v), want null", v, ok)
	}
}

func TestRoleHandler_ListIsAnArrayAndScoped(t *testing.T) {
	p := uuid.New()
	svc := &fakeRoleSvc{}
	r := newRoleRouter(svc, uuid.NewString())
	for _, path := range []string{"/admin/roles", "/projects/" + p.String() + "/roles/"} {
		w := doRole(t, r, http.MethodGet, path, nil)
		if w.Code != http.StatusOK || string(decodeRole(t, w).Data) != "[]" {
			t.Fatalf("%s: %d %s, want 200 and []", path, w.Code, w.Body.String())
		}
	}
	if svc.gotProject == nil || *svc.gotProject != p {
		t.Fatalf("project list must use the URL project: %v", svc.gotProject)
	}
	svc.roles = []*roledom.Role{sampleRole(nil), sampleRole(&p)}
	w := doRole(t, r, http.MethodGet, "/projects/"+p.String()+"/roles/", nil)
	var items []struct {
		ProjectID *string `json:"project_id"`
	}
	_ = json.Unmarshal(decodeRole(t, w).Data, &items)
	if len(items) != 2 || items[0].ProjectID != nil || items[1].ProjectID == nil {
		t.Fatalf("each item carries its project_id so the UI can tell them apart: %+v", items)
	}
}

func TestRoleHandler_CreateAndUpdate(t *testing.T) {
	p := uuid.New()
	svc := &fakeRoleSvc{role: sampleRole(nil)}
	r := newRoleRouter(svc, uuid.NewString())
	body := `{"name":"Dev","description":"d","policy":` + handlerPolicy + `}`

	w := doRole(t, r, http.MethodPost, "/admin/roles", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create platform: %d %s", w.Code, w.Body.String())
	}
	if svc.gotProject != nil || svc.gotIn.Name != "Dev" || svc.gotIn.Description != "d" {
		t.Fatalf("service input = project %v %+v", svc.gotProject, svc.gotIn)
	}
	if !jsonEqual(t, svc.gotIn.Policy, handlerPolicy) {
		t.Fatalf("policy must reach the service verbatim: %s", svc.gotIn.Policy)
	}
	if w = doRole(t, r, http.MethodPost, "/projects/"+p.String()+"/roles/", body); w.Code != http.StatusCreated || svc.gotProject == nil || *svc.gotProject != p {
		t.Fatalf("create project: %d project=%v", w.Code, svc.gotProject)
	}
	id := svc.role.ID
	if w = doRole(t, r, http.MethodPut, "/admin/roles/"+id.String(), body); w.Code != http.StatusOK || svc.gotID != id || svc.gotProject != nil {
		t.Fatalf("update: %d id=%s project=%v", w.Code, svc.gotID, svc.gotProject)
	}
	if w = doRole(t, r, http.MethodPut, "/projects/"+p.String()+"/roles/"+id.String(), body); w.Code != http.StatusOK || svc.gotProject == nil || *svc.gotProject != p {
		t.Fatalf("update in project: %d", w.Code)
	}
	// a legacy-style body carries no policy: the service answers (422), the handler does not translate it
	svc.err = apierr.NewWithIssues(apierr.CodeRolePolicyInvalid, "the policy is not valid", []apierr.Issue{{Path: "policy", Message: "policy is required"}})
	w = doRole(t, r, http.MethodPost, "/admin/roles", `{"name":"X","permissions":{"tasks.read":true}}`)
	env := decodeRole(t, w)
	if w.Code != http.StatusUnprocessableEntity || env.ErrorCode != "ROLE_POLICY_INVALID" || len(env.Issues) != 1 || env.Issues[0].Path != "policy" {
		t.Fatalf("invalid policy: %d %s", w.Code, w.Body.String())
	}
	if len(svc.gotIn.Policy) != 0 {
		t.Fatalf("legacy permissions must not be translated into a policy: %s", svc.gotIn.Policy)
	}
}

func jsonEqual(t *testing.T, raw json.RawMessage, want string) bool {
	t.Helper()
	var a, b any
	if json.Unmarshal(raw, &a) != nil || json.Unmarshal([]byte(want), &b) != nil {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func TestRoleHandler_BadInput(t *testing.T) {
	r := newRoleRouter(&fakeRoleSvc{role: sampleRole(nil)}, uuid.NewString())
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/admin/roles/nope", ""},
		{http.MethodPut, "/admin/roles/nope", `{}`},
		{http.MethodDelete, "/admin/roles/nope", ""},
		{http.MethodPut, "/admin/roles/nope/default", ""},
		{http.MethodGet, "/projects/nope/roles/", ""},
		{http.MethodPost, "/admin/roles", `{`},
		{http.MethodPut, "/admin/roles/" + uuid.NewString(), `{`},
		{http.MethodPut, "/admin/users/nope/roles", `{"role_ids":[]}`},
		{http.MethodPut, "/admin/users/" + uuid.NewString() + "/roles", `{"role_ids":["nope"]}`},
		{http.MethodPut, "/admin/agents/nope/roles", `{"role_ids":[]}`},
		{http.MethodGet, "/projects/" + uuid.NewString() + "/members/nope/roles", ""},
		{http.MethodPut, "/projects/nope/members/" + uuid.NewString() + "/roles", `{}`},
		{http.MethodPost, "/roles/validate", `{`},
		{http.MethodPost, "/roles/simulate", `{`},
	} {
		w := doRole(t, r, tc.method, tc.path, tc.body)
		if w.Code != http.StatusBadRequest || decodeRole(t, w).ErrorCode != "BAD_REQUEST" {
			t.Errorf("%s %s: %d %s, want 400 BAD_REQUEST", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestRoleHandler_ErrorMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{roledom.ErrNotFound, 404, "ROLE_NOT_FOUND"},
		{roledom.ErrNameTaken, 409, "ROLE_NAME_TAKEN"},
		{roledom.ErrNameInvalid, 400, "ROLE_NAME_INVALID"},
		{roledom.ErrSystemRole, 409, "ROLE_IS_SYSTEM"},
		{roledom.ErrIsDefault, 409, "ROLE_IS_DEFAULT"},
		{roledom.ErrLastWildcard, 409, "ROLE_LAST_FULL_ACCESS"},
		{roledom.ErrNotAttachable, 422, "ROLE_NOT_ATTACHABLE"},
		{roledom.ErrUserNotFound, 404, "USER_NOT_FOUND"},
		{roledom.ErrAgentNotFound, 404, "AGENT_NOT_FOUND"},
		{roledom.ErrProjectNotFound, 404, "PROJECT_NOT_FOUND"},
		{roledom.ErrMemberNotFound, 404, "PROJECT_MEMBER_NOT_FOUND"},
		{errors.New("db exploded"), 500, "INTERNAL_ERROR"},
	}
	id := uuid.NewString()
	for _, tc := range cases {
		r := newRoleRouter(&fakeRoleSvc{err: tc.err}, uuid.NewString())
		for _, req := range []struct{ method, path, body string }{
			{http.MethodDelete, "/admin/roles/" + id, ""},
			{http.MethodPut, "/admin/users/" + id + "/roles", `{"role_ids":[]}`},
		} {
			w := doRole(t, r, req.method, req.path, req.body)
			if w.Code != tc.status || decodeRole(t, w).ErrorCode != tc.code {
				t.Errorf("%v on %s %s: %d %s, want %d %s", tc.err, req.method, req.path, w.Code, w.Body.String(), tc.status, tc.code)
			}
		}
	}
}

func TestRoleHandler_DeleteAndSetDefault(t *testing.T) {
	svc := &fakeRoleSvc{role: sampleRole(nil)}
	r := newRoleRouter(svc, uuid.NewString())
	id := svc.role.ID
	w := doRole(t, r, http.MethodDelete, "/admin/roles/"+id.String(), nil)
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 || svc.gotID != id {
		t.Fatalf("delete: %d %q", w.Code, w.Body.String())
	}
	w = doRole(t, r, http.MethodPut, "/admin/roles/"+id.String()+"/default", nil)
	if w.Code != http.StatusOK || svc.gotID != id {
		t.Fatalf("default: %d %s", w.Code, w.Body.String())
	}
}

func TestRoleHandler_Attachments(t *testing.T) {
	caller, target, a, b, project, member := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	svc := &fakeRoleSvc{roles: []*roledom.Role{sampleRole(nil)}}
	r := newRoleRouter(svc, caller.String())
	body := map[string]any{"role_ids": []string{a.String(), b.String()}}

	check := func(name string) {
		t.Helper()
		if len(svc.gotRoleIDs) != 2 || svc.gotRoleIDs[0] != a || svc.gotRoleIDs[1] != b {
			t.Errorf("%s: role ids = %v", name, svc.gotRoleIDs)
		}
		if svc.gotCreatedBy == nil || *svc.gotCreatedBy != caller {
			t.Errorf("%s: created_by must be the caller: %v", name, svc.gotCreatedBy)
		}
	}
	for _, tc := range []struct {
		name, path string
		target     func() uuid.UUID
	}{
		{"user", "/admin/users/" + target.String() + "/roles", func() uuid.UUID { return svc.gotID }},
		{"agent", "/admin/agents/" + target.String() + "/roles", func() uuid.UUID { return svc.gotID }},
	} {
		if w := doRole(t, r, http.MethodPut, tc.path, body); w.Code != http.StatusOK {
			t.Fatalf("%s put: %d %s", tc.name, w.Code, w.Body.String())
		} else if string(decodeRole(t, w).Data) == "" || tc.target() != target {
			t.Fatalf("%s target = %s", tc.name, tc.target())
		}
		check(tc.name)
		if w := doRole(t, r, http.MethodGet, tc.path, nil); w.Code != http.StatusOK || svc.gotID != target {
			t.Fatalf("%s get: %d", tc.name, w.Code)
		}
	}
	mp := "/projects/" + project.String() + "/members/" + member.String() + "/roles"
	if w := doRole(t, r, http.MethodPut, mp, body); w.Code != http.StatusOK || *svc.gotProject != project || svc.gotMember != member {
		t.Fatalf("member put: %d", w.Code)
	}
	check("member")
	if w := doRole(t, r, http.MethodGet, mp, nil); w.Code != http.StatusOK || svc.gotMember != member {
		t.Fatalf("member get: %d", w.Code)
	}
	// the response is the resulting role list (an array of roles)
	w := doRole(t, r, http.MethodPut, mp, body)
	var items []map[string]any
	if err := json.Unmarshal(decodeRole(t, w).Data, &items); err != nil || len(items) != 1 || items[0]["attachment_count"] != float64(3) {
		t.Fatalf("response = %s err=%v", w.Body.String(), err)
	}
}

func TestRoleHandler_Catalogue(t *testing.T) {
	r := newRoleRouter(&fakeRoleSvc{}, uuid.NewString())
	w := doRole(t, r, http.MethodGet, "/roles/actions", nil)
	var actions []string
	if err := json.Unmarshal(decodeRole(t, w).Data, &actions); err != nil || len(actions) != 2 || actions[0] != "tasks:read" {
		t.Fatalf("actions: %s err=%v", w.Body.String(), err)
	}
	w = doRole(t, r, http.MethodGet, "/roles/attribute-schema", nil)
	var defs []map[string]any
	if err := json.Unmarshal(decodeRole(t, w).Data, &defs); err != nil || len(defs) != 1 {
		t.Fatalf("schema: %s err=%v", w.Body.String(), err)
	}
	d := defs[0]
	if d["key"] != "doc.ancestor_folder_ids" || d["resource_kind"] != "doc" || d["type"] != "string" || d["multi_valued"] != true || d["label_key"] == "" {
		t.Fatalf("def shape = %v", d)
	}
}

func TestRoleHandler_Validate(t *testing.T) {
	svc := &fakeRoleSvc{}
	r := newRoleRouter(svc, uuid.NewString())
	w := doRole(t, r, http.MethodPost, "/roles/validate", `{"policy":`+handlerPolicy+`}`)
	var got struct {
		Valid  bool                `json:"valid"`
		Issues []map[string]string `json:"issues"`
	}
	if err := json.Unmarshal(decodeRole(t, w).Data, &got); err != nil || w.Code != http.StatusOK || !got.Valid || got.Issues == nil || len(got.Issues) != 0 {
		t.Fatalf("valid policy: %d %s", w.Code, w.Body.String())
	}
	svc.issues = []roledom.Issue{{Path: "statements[0].actions[0]", Message: "unknown action"}}
	w = doRole(t, r, http.MethodPost, "/roles/validate", `{"policy":{}}`)
	got.Issues = nil
	if err := json.Unmarshal(decodeRole(t, w).Data, &got); err != nil || w.Code != http.StatusOK || got.Valid || len(got.Issues) != 1 ||
		got.Issues[0]["path"] != "statements[0].actions[0]" || got.Issues[0]["message"] != "unknown action" {
		t.Fatalf("invalid policy is still a 200 with issues: %d %s", w.Code, w.Body.String())
	}
}

func TestRoleHandler_Simulate(t *testing.T) {
	uid := uuid.NewString()
	svc := &fakeRoleSvc{sim: &roledom.SimulationResult{Allowed: true, Matched: []roledom.Matched{
		{RoleID: "policy", Sid: "S", Effect: "Allow", Index: 0}, {RoleID: uuid.NewString(), Effect: "Deny", Index: 2},
	}}}
	r := newRoleRouter(svc, uuid.NewString())
	body := `{"policy":` + handlerPolicy + `,"principal":{"type":"user","id":"` + uid + `"},"action":"tasks:read","resource":"project/p/task/t",` +
		`"attributes":{"task.sprint_id":"s1","doc.ancestor_folder_ids":["a","b"]}}`
	w := doRole(t, r, http.MethodPost, "/roles/simulate", body)
	if w.Code != http.StatusOK {
		t.Fatalf("simulate: %d %s", w.Code, w.Body.String())
	}
	if svc.gotSim.Principal == nil || svc.gotSim.Principal.Type != "user" || svc.gotSim.Principal.ID != uid ||
		svc.gotSim.Action != "tasks:read" || svc.gotSim.Resource != "project/p/task/t" {
		t.Fatalf("input = %+v", svc.gotSim)
	}
	if got := svc.gotSim.Attributes["task.sprint_id"]; len(got) != 1 || got[0] != "s1" {
		t.Fatalf("scalar attribute = %v", got)
	}
	if got := svc.gotSim.Attributes["doc.ancestor_folder_ids"]; len(got) != 2 {
		t.Fatalf("list attribute = %v", got)
	}
	var resp struct {
		Allowed bool `json:"allowed"`
		Matched []struct {
			RoleID string `json:"role_id"`
			Sid    string `json:"sid"`
			Effect string `json:"effect"`
			Index  int    `json:"index"`
		} `json:"matched"`
	}
	if err := json.Unmarshal(decodeRole(t, w).Data, &resp); err != nil || !resp.Allowed || len(resp.Matched) != 2 ||
		resp.Matched[0].RoleID != "policy" || resp.Matched[0].Sid != "S" || resp.Matched[1].Effect != "Deny" || resp.Matched[1].Index != 2 {
		t.Fatalf("response = %s err=%v", w.Body.String(), err)
	}
	// no principal: the policy alone
	svc.gotSim = roledom.SimulateInput{}
	doRole(t, r, http.MethodPost, "/roles/simulate", `{"policy":`+handlerPolicy+`,"action":"a:b","resource":"*"}`)
	if svc.gotSim.Principal != nil || svc.gotSim.Attributes != nil {
		t.Fatalf("input = %+v", svc.gotSim)
	}
	// matched is [] and never null
	svc.sim = &roledom.SimulationResult{}
	w = doRole(t, r, http.MethodPost, "/roles/simulate", `{"policy":{},"action":"a:b","resource":"*"}`)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(decodeRole(t, w).Data, &raw)
	if string(raw["matched"]) != "[]" {
		t.Fatalf("matched = %s", raw["matched"])
	}
}
