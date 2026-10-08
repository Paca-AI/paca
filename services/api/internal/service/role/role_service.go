// Package rolesvc implements the IAM role and attachment use cases.
//
// The service validates input and protects data-integrity invariants (system
// roles are read-only, the default role stays, the last platform-wide full
// access attachment stays, names are unique per scope, attachments name
// existing roles in an allowed scope and live principals). It makes NO
// authorization decisions: who may call each use case is decided by the route
// gates, the roles:assign gate and the escalation guards in the HTTP middleware.
package rolesvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

const maxNameLen = 100

// Invalidator drops cached policies of roles. *iam.Authorizer implements it.
type Invalidator interface {
	Invalidate(roleIDs ...string)
}

// Simulator evaluates a what-if request. *iam.Authorizer implements it.
type Simulator interface {
	Simulate(ctx context.Context, p *iam.Principal, policy *iam.Policy, action, resource string, attrs map[string][]string) (iam.Result, error)
}

// MembersCache drops a project's cached member list (which shows each
// member's roles). projectsvc.CachedService implements it.
type MembersCache interface {
	InvalidateMembersCache(ctx context.Context, projectID uuid.UUID) error
}

// WithMembersCache wires in the member-list cache invalidation run after a
// member's roles change or a project role is renamed or deleted. Roles shared
// by many projects are not covered: their name shows up on the cached member
// lists of other projects until the cache entry expires.
func (s *Service) WithMembersCache(c MembersCache) *Service {
	s.members = c
	return s
}

func (s *Service) membersChanged(ctx context.Context, projectID *uuid.UUID) {
	if s.members != nil && projectID != nil {
		_ = s.members.InvalidateMembersCache(ctx, *projectID)
	}
}

// Service is the role service.
type Service struct {
	members MembersCache
	repo    roledom.Repository
	inv     Invalidator
	sim     Simulator
	reg     *iam.Registry
	schema  *iam.AttributeSchema
}

var _ roledom.Service = (*Service)(nil)

// New returns a role service. reg and schema validate policies and feed the
// catalogue endpoints; inv is called after every committed change of a role's
// policy or attachments.
func New(repo roledom.Repository, inv Invalidator, sim Simulator, reg *iam.Registry, schema *iam.AttributeSchema) *Service {
	return &Service{repo: repo, inv: inv, sim: sim, reg: reg, schema: schema}
}

// ----------------------------------------------------------------------------
// Roles

// ListPlatform implements roledom.Service.
func (s *Service) ListPlatform(ctx context.Context) ([]*roledom.Role, error) {
	return s.repo.ListPlatform(ctx)
}

// ListForProject implements roledom.Service.
func (s *Service) ListForProject(ctx context.Context, projectID uuid.UUID) ([]*roledom.Role, error) {
	ok, err := s.repo.ProjectExists(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, roledom.ErrProjectNotFound
	}
	return s.repo.ListForProject(ctx, projectID)
}

// Get implements roledom.Service. A platform scope sees platform roles only; a
// project scope sees the project's own roles and the platform roles.
func (s *Service) Get(ctx context.Context, projectID *uuid.UUID, id uuid.UUID) (*roledom.Role, error) {
	role, err := s.repo.FindByID(ctx, id, projectID)
	if err != nil {
		return nil, err
	}
	if role.ProjectID != nil && (projectID == nil || *role.ProjectID != *projectID) {
		return nil, roledom.ErrNotFound
	}
	return role, nil
}

// Create implements roledom.Service.
func (s *Service) Create(ctx context.Context, projectID *uuid.UUID, in roledom.RoleInput) (*roledom.Role, error) {
	name, err := validName(in.Name)
	if err != nil {
		return nil, err
	}
	policy, err := s.normalizePolicy(in.Policy, projectID)
	if err != nil {
		return nil, err
	}
	if projectID != nil {
		ok, err := s.repo.ProjectExists(ctx, *projectID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, roledom.ErrProjectNotFound
		}
	}
	role := &roledom.Role{Name: name, Description: strings.TrimSpace(in.Description), Policy: policy, ProjectID: projectID}
	if err := s.repo.Create(ctx, role); err != nil {
		return nil, err
	}
	return role, nil
}

// Update implements roledom.Service.
func (s *Service) Update(ctx context.Context, projectID *uuid.UUID, id uuid.UUID, in roledom.RoleInput) (*roledom.Role, error) {
	role, err := s.ownedRole(ctx, projectID, id)
	if err != nil {
		return nil, err
	}
	name, err := validName(in.Name)
	if err != nil {
		return nil, err
	}
	policy, err := s.normalizePolicy(in.Policy, projectID)
	if err != nil {
		return nil, err
	}
	role.Name, role.Description, role.Policy = name, strings.TrimSpace(in.Description), policy
	if err := s.repo.Update(ctx, role); err != nil {
		return nil, err
	}
	s.inv.Invalidate(id.String())
	s.membersChanged(ctx, projectID)
	return s.repo.FindByID(ctx, id, projectID)
}

// Delete implements roledom.Service.
func (s *Service) Delete(ctx context.Context, projectID *uuid.UUID, id uuid.UUID) error {
	role, err := s.ownedRole(ctx, projectID, id)
	if err != nil {
		return err
	}
	if role.IsSystem {
		return roledom.ErrSystemRole
	}
	if role.IsDefault {
		return roledom.ErrIsDefault
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	s.inv.Invalidate(id.String())
	s.membersChanged(ctx, projectID)
	return nil
}

// SetDefault implements roledom.Service.
func (s *Service) SetDefault(ctx context.Context, id uuid.UUID) (*roledom.Role, error) {
	role, err := s.repo.FindByID(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	if role.ProjectID != nil {
		return nil, roledom.ErrNotFound // only a platform role can be the default
	}
	if role.IsProjectTemplate() {
		// New users start with the default role platform-wide, which would give
		// them the template's powers in every project.
		return nil, fmt.Errorf("%w: role %s is a project template", roledom.ErrNotAttachable, id)
	}
	if err := s.repo.SetDefault(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, id, nil)
}

// ownedRole loads a role that must belong to the addressed scope exactly
// (platform: no owner project; project: owned by that project).
func (s *Service) ownedRole(ctx context.Context, projectID *uuid.UUID, id uuid.UUID) (*roledom.Role, error) {
	role, err := s.repo.FindByID(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	switch {
	case projectID == nil && role.ProjectID != nil:
		return nil, roledom.ErrNotFound
	case projectID != nil && (role.ProjectID == nil || *role.ProjectID != *projectID):
		return nil, roledom.ErrNotFound
	}
	return role, nil
}

func validName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return "", roledom.ErrNameInvalid
	}
	return name, nil
}

// ----------------------------------------------------------------------------
// Attachments

// ListUserRoles implements roledom.Service.
func (s *Service) ListUserRoles(ctx context.Context, userID uuid.UUID) ([]*roledom.Role, error) {
	if err := s.requireUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.repo.ListAttached(ctx, roledom.PrincipalUser, userID, nil)
}

// ReplaceUserRoles implements roledom.Service.
func (s *Service) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) ([]*roledom.Role, error) {
	if err := s.requireUser(ctx, userID); err != nil {
		return nil, err
	}
	return s.replace(ctx, roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalUser, PrincipalID: userID, RoleIDs: dedupe(roleIDs), CreatedBy: createdBy,
	})
}

// ListAgentRoles implements roledom.Service.
func (s *Service) ListAgentRoles(ctx context.Context, agentID uuid.UUID) ([]*roledom.Role, error) {
	if err := s.requireGlobalAgent(ctx, agentID); err != nil {
		return nil, err
	}
	return s.repo.ListAttached(ctx, roledom.PrincipalAgent, agentID, nil)
}

// ReplaceAgentRoles implements roledom.Service.
func (s *Service) ReplaceAgentRoles(ctx context.Context, agentID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) ([]*roledom.Role, error) {
	if err := s.requireGlobalAgent(ctx, agentID); err != nil {
		return nil, err
	}
	return s.replace(ctx, roledom.ReplaceAttachmentsInput{
		PrincipalType: roledom.PrincipalAgent, PrincipalID: agentID, RoleIDs: dedupe(roleIDs), CreatedBy: createdBy,
	})
}

// ListMemberRoles implements roledom.Service.
func (s *Service) ListMemberRoles(ctx context.Context, projectID, memberID uuid.UUID) ([]*roledom.Role, error) {
	m, err := s.member(ctx, projectID, memberID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListAttached(ctx, m.PrincipalType, m.PrincipalID, &projectID)
}

// ReplaceMemberRoles implements roledom.Service.
func (s *Service) ReplaceMemberRoles(ctx context.Context, projectID, memberID uuid.UUID, roleIDs []uuid.UUID, createdBy *uuid.UUID) ([]*roledom.Role, error) {
	m, err := s.member(ctx, projectID, memberID)
	if err != nil {
		return nil, err
	}
	roles, err := s.replace(ctx, roledom.ReplaceAttachmentsInput{
		PrincipalType: m.PrincipalType, PrincipalID: m.PrincipalID, ProjectID: &projectID,
		RoleIDs: dedupe(roleIDs), CreatedBy: createdBy,
	})
	if err != nil {
		return nil, err
	}
	s.membersChanged(ctx, &projectID)
	return roles, nil
}

func (s *Service) requireUser(ctx context.Context, id uuid.UUID) error {
	ok, err := s.repo.UserExists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return roledom.ErrUserNotFound
	}
	return nil
}

func (s *Service) requireGlobalAgent(ctx context.Context, id uuid.UUID) error {
	ok, err := s.repo.GlobalAgentExists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return roledom.ErrAgentNotFound
	}
	return nil
}

func (s *Service) member(ctx context.Context, projectID, memberID uuid.UUID) (*roledom.Member, error) {
	ok, err := s.repo.ProjectExists(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, roledom.ErrProjectNotFound
	}
	return s.repo.FindMember(ctx, projectID, memberID)
}

// replace validates that every role exists and may be attached in the scope,
// applies the replace-set and invalidates the changed roles' cached policies.
func (s *Service) replace(ctx context.Context, in roledom.ReplaceAttachmentsInput) ([]*roledom.Role, error) {
	if err := s.checkAttachable(ctx, in.RoleIDs, in.ProjectID); err != nil {
		return nil, err
	}
	changed, err := s.repo.ReplaceAttachments(ctx, in)
	if err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		ids := make([]string, len(changed))
		for i, id := range changed {
			ids[i] = id.String()
		}
		s.inv.Invalidate(ids...)
	}
	return s.repo.ListAttached(ctx, in.PrincipalType, in.PrincipalID, in.ProjectID)
}

// checkAttachable rejects unknown role ids and roles that cannot be attached
// in the scope: platform roles attach anywhere, a project-owned role only in
// its own project.
func (s *Service) checkAttachable(ctx context.Context, ids []uuid.UUID, scope *uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	found, err := s.repo.FindByIDs(ctx, ids)
	if err != nil {
		return err
	}
	byID := make(map[uuid.UUID]*roledom.Role, len(found))
	for _, r := range found {
		byID[r.ID] = r
	}
	for _, id := range ids {
		r, ok := byID[id]
		switch {
		case !ok:
			return fmt.Errorf("%w: unknown role %s", roledom.ErrNotAttachable, id)
		case r.ProjectID != nil && (scope == nil || *r.ProjectID != *scope):
			return fmt.Errorf("%w: role %s belongs to a project and can only be attached inside it", roledom.ErrNotAttachable, id)
		case scope == nil && r.IsProjectTemplate():
			return fmt.Errorf("%w: role %s is a project template and can only be attached inside a project", roledom.ErrNotAttachable, id)
		}
	}
	return nil
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// Catalogue, validation, simulation (pure; no data is read)

// Actions implements roledom.Service.
func (s *Service) Actions() []string { return s.reg.Actions() }

// AttributeDefs implements roledom.Service.
func (s *Service) AttributeDefs() []roledom.AttributeDef {
	defs := s.schema.Defs()
	out := make([]roledom.AttributeDef, len(defs))
	for i, d := range defs {
		out[i] = roledom.AttributeDef{
			Key: d.Key, ResourceKind: d.ResourceKind, Type: string(d.Type), MultiValued: d.MultiValued, LabelKey: d.LabelKey,
		}
	}
	return out
}

// ValidatePolicy implements roledom.Service.
func (s *Service) ValidatePolicy(raw json.RawMessage, projectID *uuid.UUID) []roledom.Issue {
	_, issues := s.parseAndValidate(raw, projectID)
	out := make([]roledom.Issue, len(issues))
	for i, is := range issues {
		out[i] = roledom.Issue{Path: is.Path, Message: is.Message}
	}
	return out
}

// Simulate implements roledom.Service.
func (s *Service) Simulate(ctx context.Context, in roledom.SimulateInput) (*roledom.SimulationResult, error) {
	if strings.TrimSpace(in.Action) == "" || strings.TrimSpace(in.Resource) == "" {
		return nil, apierr.New(apierr.CodeBadRequest, "action and resource are required")
	}
	policy, err := s.normalizedParsed(in.Policy, nil)
	if err != nil {
		return nil, err
	}
	var principal *iam.Principal
	if in.Principal != nil {
		id, perr := uuid.Parse(in.Principal.ID)
		if perr != nil || (in.Principal.Type != roledom.PrincipalUser && in.Principal.Type != roledom.PrincipalAgent) {
			return nil, apierr.New(apierr.CodeBadRequest, "principal must be {type: user|agent, id: uuid}")
		}
		principal = &iam.Principal{Type: in.Principal.Type, ID: id.String()}
	}
	res, err := s.sim.Simulate(ctx, principal, policy, in.Action, in.Resource, in.Attributes)
	if err != nil {
		return nil, err
	}
	out := &roledom.SimulationResult{Allowed: res.Allowed, Matched: make([]roledom.Matched, len(res.Matched))}
	for i, m := range res.Matched {
		out.Matched[i] = roledom.Matched{RoleID: m.RoleID, Sid: m.Sid, Effect: string(m.Effect), Index: m.Index}
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// Policy handling

// parseAndValidate parses raw as an IAM policy and validates it against the
// action registry and attribute schema. The returned policy is nil when
// parsing failed.
func (s *Service) parseAndValidate(raw json.RawMessage, projectID *uuid.UUID) (*iam.Policy, []iam.Issue) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, []iam.Issue{{Path: "policy", Message: "policy is required"}}
	}
	p, err := iam.ParsePolicy(trimmed)
	if err != nil {
		return nil, []iam.Issue{{Path: "policy", Message: err.Error()}}
	}
	issues := iam.Validate(p, s.reg, s.schema)
	if projectID != nil && len(issues) == 0 {
		issues = append(issues, projectResourceIssues(p, *projectID)...)
	}
	return p, issues
}

// projectResourceIssues reports every resource of a project-owned role's
// policy that lies outside its own project. Such a role is attached only
// inside that project, so a resource naming anything else (another project,
// every project, the platform) could never apply and would only mislead.
func projectResourceIssues(p *iam.Policy, projectID uuid.UUID) []iam.Issue {
	root := "project/" + projectID.String()
	var out []iam.Issue
	for i, st := range p.Statements {
		for j, res := range st.Resources {
			if res == root || strings.HasPrefix(res, root+"/") {
				continue
			}
			out = append(out, iam.Issue{
				Path:    fmt.Sprintf("statements[%d].resources[%d]", i, j),
				Message: fmt.Sprintf("a project role's resources must be inside %s (for example %s/*)", root, root),
			})
		}
	}
	return out
}

func issuesError(issues []iam.Issue) error {
	out := make([]apierr.Issue, len(issues))
	for i, is := range issues {
		out[i] = apierr.Issue{Path: is.Path, Message: is.Message}
	}
	return apierr.NewWithIssues(apierr.CodeRolePolicyInvalid, "the policy is not valid", out)
}

// normalizedParsed returns the validated policy or a 422 error with issues.
func (s *Service) normalizedParsed(raw json.RawMessage, projectID *uuid.UUID) (*iam.Policy, error) {
	p, issues := s.parseAndValidate(raw, projectID)
	if len(issues) > 0 {
		return nil, issuesError(issues)
	}
	return p, nil
}

// normalizePolicy validates raw and returns its canonical JSON form for
// storage.
func (s *Service) normalizePolicy(raw json.RawMessage, projectID *uuid.UUID) (json.RawMessage, error) {
	p, err := s.normalizedParsed(raw, projectID)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("role service: encode policy: %w", err)
	}
	return b, nil
}
