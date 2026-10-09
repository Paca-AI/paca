package dto

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	roledom "github.com/Paca-AI/api/internal/domain/role"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// --- IAM role DTOs -----------------------------------------------------------
//
// The roles API speaks IAM policy JSON only: a role's `policy` is the policy
// document itself (version + statements), never a permission map.

// RoleRequest is the body of POST /admin/roles, PUT /admin/roles/{roleId} and
// their /projects/{projectId}/roles counterparts.
type RoleRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Policy is the IAM policy document, kept as raw JSON: the service parses
	// and validates it, and the escalation guard in the middleware reads the
	// same bytes.
	Policy json.RawMessage `json:"policy"`
}

// ReplaceRolesRequest is the body of the PUT .../roles assignment routes: the
// complete set of roles the principal should hold in that scope.
type ReplaceRolesRequest struct {
	RoleIDs []uuid.UUID `json:"role_ids"`
}

// ValidatePolicyRequest is the body of POST /roles/validate.
type ValidatePolicyRequest struct {
	Policy json.RawMessage `json:"policy"`
}

// SimulatePrincipal names the principal of a simulation.
type SimulatePrincipal struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// SimulateRequest is the body of POST /roles/simulate. Attribute values may be
// given as a string, a bool or a list of strings.
type SimulateRequest struct {
	Policy     json.RawMessage          `json:"policy"`
	Principal  *SimulatePrincipal       `json:"principal"`
	Action     string                   `json:"action"`
	Resource   string                   `json:"resource"`
	Attributes map[string]iam.ValueList `json:"attributes"`
}

// RoleResponse is the public representation of a role.
type RoleResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	// Policy is the policy document as a JSON object.
	Policy json.RawMessage `json:"policy"`
	// ProjectID is the owning project, or null for a platform role.
	ProjectID *uuid.UUID `json:"project_id"`
	IsSystem  bool       `json:"is_system"`
	IsDefault bool       `json:"is_default"`
	// AttachmentCount is how many attachments reference the role: all of
	// them in platform listings, those inside the project in project listings.
	AttachmentCount int       `json:"attachment_count"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// RoleFromEntity maps a role to its response.
func RoleFromEntity(r *roledom.Role) RoleResponse {
	policy := r.Policy
	if len(policy) == 0 {
		policy = json.RawMessage(`{"statements":[]}`)
	}
	return RoleResponse{
		ID: r.ID, Name: r.Name, Description: r.Description, Policy: policy, ProjectID: r.ProjectID,
		IsSystem: r.IsSystem, IsDefault: r.IsDefault, AttachmentCount: r.AttachmentCount,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// RolesFromEntities maps roles to responses (never null).
func RolesFromEntities(roles []*roledom.Role) []RoleResponse {
	out := make([]RoleResponse, 0, len(roles))
	for _, r := range roles {
		out = append(out, RoleFromEntity(r))
	}
	return out
}

// RoleSummaryResponse identifies a role attached to a user, member or agent.
type RoleSummaryResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// RoleSummariesFromEntities maps role summaries; the result is never nil.
func RoleSummariesFromEntities(in []roledom.Summary) []RoleSummaryResponse {
	out := make([]RoleSummaryResponse, 0, len(in))
	for _, r := range in {
		out = append(out, RoleSummaryResponse{ID: r.ID, Name: r.Name})
	}
	return out
}

// PolicyIssueResponse is one located policy validation problem.
type PolicyIssueResponse struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidatePolicyResponse is the result of POST /roles/validate.
type ValidatePolicyResponse struct {
	Valid  bool                  `json:"valid"`
	Issues []PolicyIssueResponse `json:"issues"`
}

// MatchedStatementResponse is a statement that matched a simulated request;
// RoleID is "policy" for a statement of the policy under test.
type MatchedStatementResponse struct {
	RoleID string `json:"role_id"`
	Sid    string `json:"sid"`
	Effect string `json:"effect"`
	Index  int    `json:"index"`
}

// SimulateResponse is the result of POST /roles/simulate.
type SimulateResponse struct {
	Allowed bool                       `json:"allowed"`
	Matched []MatchedStatementResponse `json:"matched"`
}

// AttributeDefResponse describes one condition attribute.
type AttributeDefResponse struct {
	Key          string `json:"key"`
	ResourceKind string `json:"resource_kind"`
	Type         string `json:"type"`
	MultiValued  bool   `json:"multi_valued"`
	LabelKey     string `json:"label_key"`
}
