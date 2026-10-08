// Package defaultroles holds the roles Paca ships, as embedded IAM policy
// documents.
//
// Two kinds exist:
//
//   - platform roles (SUPER_ADMIN, ADMIN, USER) are system roles: the API
//     seeds them at startup and keeps their stored policy equal to the shipped
//     one;
//   - project templates (Admin, Editor, Viewer) are instantiated once
//     per project as roles owned by that project.
//
// manifest.json lists the roles (name, description, kind, flags) and names
// the policy file of each; every policy file is a plain IAM policy document
// that iam.ParsePolicy and iam.Validate accept. The one extension is in the
// project templates, which write the literal token ProjectIDToken wherever a
// resource names the project. Instantiate replaces the token with the real
// project id BEFORE the policy is stored, so stored policies are plain IAM
// with real ids and the evaluator needs no variables.
package defaultroles

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

//go:embed manifest.json policies/*.json
var files embed.FS

// ProjectIDToken is the placeholder a project template's resources use for the
// owning project's id.
const ProjectIDToken = "PROJECT_ID"

// Kind says where a shipped role lives.
type Kind string

const (
	// KindPlatform roles have no owner project and can be attached
	// platform-wide.
	KindPlatform Kind = "platform"
	// KindProject roles are templates instantiated per project.
	KindProject Kind = "project"
)

// Names of the roles the rest of the API refers to.
const (
	SuperAdmin = "SUPER_ADMIN"
	Admin      = "ADMIN"
	User       = "USER"

	// ProjectAdmin is the template the creator of a project holds.
	ProjectAdmin = "Admin"
)

// Role is one shipped role.
type Role struct {
	Name        string
	Description string
	Kind        Kind
	// System roles cannot be edited or deleted through the API.
	System bool
	// Default marks the platform role new accounts start with.
	Default bool
	// Policy is the policy document. For a project template it still contains
	// ProjectIDToken; Instantiate returns a copy with the token replaced.
	Policy json.RawMessage
}

type manifest struct {
	Roles []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Kind        Kind   `json:"kind"`
		Policy      string `json:"policy"`
		System      bool   `json:"system"`
		Default     bool   `json:"default"`
	} `json:"roles"`
}

var loaded = sync.OnceValue(func() []Role {
	raw, err := files.ReadFile("manifest.json")
	if err != nil {
		panic(fmt.Sprintf("defaultroles: read manifest: %v", err))
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		panic(fmt.Sprintf("defaultroles: parse manifest: %v", err))
	}
	out := make([]Role, 0, len(m.Roles))
	for _, e := range m.Roles {
		policy, err := files.ReadFile(e.Policy)
		if err != nil {
			panic(fmt.Sprintf("defaultroles: role %s: read %s: %v", e.Name, e.Policy, err))
		}
		switch e.Kind {
		case KindPlatform, KindProject:
		default:
			panic(fmt.Sprintf("defaultroles: role %s: unknown kind %q", e.Name, e.Kind))
		}
		out = append(out, Role{
			Name: e.Name, Description: e.Description, Kind: e.Kind,
			System: e.System, Default: e.Default, Policy: json.RawMessage(policy),
		})
	}
	return out
})

func ofKind(k Kind) []Role {
	var out []Role
	for _, r := range loaded() {
		if r.Kind == k {
			out = append(out, r)
		}
	}
	return out
}

// Platform returns the shipped platform roles in manifest order.
func Platform() []Role { return ofKind(KindPlatform) }

// ProjectTemplates returns the project role templates in manifest order, with
// ProjectIDToken still in their policies.
func ProjectTemplates() []Role { return ofKind(KindProject) }

// Instantiate returns the project templates as concrete roles of the project:
// every ProjectIDToken in the policies is replaced by the project's id.
func Instantiate(projectID uuid.UUID) []Role {
	out := ProjectTemplates()
	id := projectID.String()
	for i := range out {
		out[i].Policy = json.RawMessage(strings.ReplaceAll(string(out[i].Policy), ProjectIDToken, id))
	}
	return out
}
