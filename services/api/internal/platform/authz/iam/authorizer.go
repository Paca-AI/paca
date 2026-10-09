package iam

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Principal is who is asking: a user or an agent, identified by its
// lowercase UUID string.
type Principal struct {
	Type string // "user" | "agent"
	ID   string
}

func (p Principal) valid() bool {
	return p.ID != "" && (p.Type == "user" || p.Type == "agent")
}

// Store loads a principal's active role grants (parsed policies). A stored
// policy that does not parse must come back as a Grant with a nil Policy,
// which grants nothing, rather than as an error.
type Store interface {
	ListGrants(ctx context.Context, p Principal) ([]Grant, error)
}

// Invalidator is optionally implemented by a Store that caches parsed role
// policies; Authorizer.Invalidate delegates to it.
type Invalidator interface {
	Invalidate(roleIDs ...string)
}

// ErrResourceNotInProject is returned by a loader when the resource does not
// exist or does not belong to the project named in the resource string. The
// Authorizer turns exactly this error into a deny (never into empty
// attributes, which would satisfy negated conditions).
var ErrResourceNotInProject = errors.New("iam: resource not found in project")

// AttributeLoader loads condition attributes for one resource kind. Load
// returns values only for the requested keys. projectID is the project from
// the resource string ("" for platform-level resources, which skips the
// project check); a missing resource or one in another project must yield
// ErrResourceNotInProject.
type AttributeLoader interface {
	Kind() string
	Load(ctx context.Context, resourceID, projectID string, keys []string) (map[string][]string, error)
}

// AttributeExpander is optionally implemented by a loader whose attributes
// are derived from one another (a document's ancestor folders follow from its
// folder). AuthorizeChange passes the caller's new attributes through Expand
// so callers only supply the primary attribute. The result must include
// newAttrs' keys, with derived keys filled in.
type AttributeExpander interface {
	Expand(ctx context.Context, resourceID, projectID string, newAttrs map[string][]string) (map[string][]string, error)
}

// AuthzOption customises one Authorize call.
type AuthzOption func(*authzOptions)

type authzOptions struct {
	attrs map[string][]string
}

// WithAttrs supplies attribute values that OVERRIDE loaded ones for the same
// keys. Use it for creates, where the resource does not exist yet and the
// attributes come from the request.
func WithAttrs(attrs map[string][]string) AuthzOption {
	return func(o *authzOptions) { o.attrs = attrs }
}

// Authorizer answers "may principal do action on resource".
type Authorizer struct {
	store  Store
	reg    *Registry
	schema *AttributeSchema

	mu      sync.RWMutex
	loaders map[string]AttributeLoader
}

// NewAuthorizer returns an Authorizer over store.
func NewAuthorizer(store Store, reg *Registry, schema *AttributeSchema) *Authorizer {
	return &Authorizer{store: store, reg: reg, schema: schema, loaders: map[string]AttributeLoader{}}
}

// RegisterLoader registers (or replaces) the loader for l.Kind().
func (a *Authorizer) RegisterLoader(l AttributeLoader) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.loaders[l.Kind()] = l
}

// Invalidate drops cached policies of the given roles (when the store caches).
// Call it after the role update has committed, otherwise a concurrent request
// may re-cache the pre-commit policy.
func (a *Authorizer) Invalidate(roleIDs ...string) {
	if inv, ok := a.store.(Invalidator); ok {
		inv.Invalidate(roleIDs...)
	}
}

// Authorize evaluates one request. An error means the answer is unknown and
// the caller MUST deny. WithAttrs overrides only attributes of the resource's
// own kind (never principal.* or resource.id).
func (a *Authorizer) Authorize(ctx context.Context, p Principal, action, resource string, opts ...AuthzOption) (Result, error) {
	var o authzOptions
	for _, opt := range opts {
		opt(&o)
	}
	if !p.valid() {
		return Result{}, nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return Result{}, fmt.Errorf("iam: list grants: %w", err)
	}
	return a.evaluate(ctx, grants, p, action, resource, o.attrs)
}

// AuthorizeChange authorizes an update that may change attributes: it must be
// allowed against the current (loaded) state AND against the state with
// newAttrs overlaid. If the kind's loader implements AttributeExpander, newAttrs
// is expanded first (e.g. doc.folder_id -> doc.ancestor_folder_ids). Grants are
// fetched once for both evaluations.
func (a *Authorizer) AuthorizeChange(ctx context.Context, p Principal, action, resource string, newAttrs map[string][]string) (Result, error) {
	if !p.valid() {
		return Result{}, nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return Result{}, fmt.Errorf("iam: list grants: %w", err)
	}
	old, err := a.evaluate(ctx, grants, p, action, resource, nil)
	if err != nil || !old.Allowed {
		return old, err
	}
	if kind, id, proj := ParseResource(resource); kind != "" && id != "" && id != "*" {
		if l, ok := a.loader(kind); ok {
			if ex, ok := l.(AttributeExpander); ok {
				newAttrs, err = ex.Expand(ctx, id, proj, newAttrs)
				if errors.Is(err, ErrResourceNotInProject) {
					return Result{}, nil
				}
				if err != nil {
					return Result{}, fmt.Errorf("iam: expand %s attributes: %w", kind, err)
				}
			}
		}
	}
	return a.evaluate(ctx, grants, p, action, resource, newAttrs)
}

// AuthorizeCreate authorizes creating a resource: resource names the kind's
// collection ("project/<P>/task/*"), because the resource does not exist yet,
// and attrs are the attributes the request would give it. Derived attributes
// (a document's ancestor folders) are filled in through the kind's loader.
// An attribute the request leaves out is absent, so a role limited to
// "tasks in Sprint 5" cannot create a task that is in no sprint.
func (a *Authorizer) AuthorizeCreate(ctx context.Context, p Principal, action, resource string, attrs map[string][]string) (Result, error) {
	if !p.valid() {
		return Result{}, nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return Result{}, fmt.Errorf("iam: list grants: %w", err)
	}
	if kind, _, proj := ParseResource(resource); kind != "" {
		if l, ok := a.loader(kind); ok {
			if ex, ok := l.(AttributeExpander); ok {
				attrs, err = ex.Expand(ctx, "", proj, attrs)
				if errors.Is(err, ErrResourceNotInProject) {
					return Result{}, nil
				}
				if err != nil {
					return Result{}, fmt.Errorf("iam: expand %s attributes: %w", kind, err)
				}
			}
		}
	}
	return a.evaluate(ctx, grants, p, action, resource, attrs)
}

func (a *Authorizer) loader(kind string) (AttributeLoader, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	l, ok := a.loaders[kind]
	return l, ok
}

// evaluate runs Evaluate over already-fetched grants, loading exactly the
// referenced attributes of the resource's kind that are not overridden.
func (a *Authorizer) evaluate(ctx context.Context, grants []Grant, p Principal, action, resource string, over map[string][]string) (Result, error) {
	kind, id, projectID := ParseResource(resource)
	attrs := map[string][]string{
		"principal.id":   {p.ID},
		"principal.type": {p.Type},
	}
	loadable := id != "" && id != "*"
	if loadable {
		attrs["resource.id"] = []string{id}
	}
	if kind != "" && loadable {
		var need []string
		for key := range ReferencedConditionKeys(grants) {
			def, ok := a.schema.Lookup(key)
			if !ok || def.ResourceKind == "" || def.ResourceKind != kind {
				continue
			}
			if _, overridden := over[key]; overridden {
				continue // (an override only ever applies to own-kind keys, as need is)
			}
			need = append(need, key)
		}
		if len(need) > 0 {
			l, ok := a.loader(kind)
			if !ok {
				return Result{}, fmt.Errorf("iam: no attribute loader registered for kind %q", kind)
			}
			loaded, err := l.Load(ctx, id, projectID, need)
			if errors.Is(err, ErrResourceNotInProject) {
				return Result{}, nil // deny, not an error
			}
			if err != nil {
				return Result{}, fmt.Errorf("iam: load %s attributes: %w", kind, err)
			}
			for _, k := range need {
				if v, ok := loaded[k]; ok {
					attrs[k] = v
				}
			}
		}
	}
	for k, v := range over {
		// Only the resource's own kind's attributes may be overridden; a
		// forwarded request map must never set principal.* or resource.id.
		if def, ok := a.schema.Lookup(k); ok && def.ResourceKind != "" && def.ResourceKind == kind {
			attrs[k] = v
		}
	}
	return Evaluate(grants, Request{Action: action, Resource: resource, Attrs: attrs}), nil
}

// ListScope compiles the principal's grants into a predicate over the
// resources of kind in projectID that action is allowed on. The repository
// applies it inside the list query, so filtering happens before pagination,
// counts and sums rather than after. An error means the scope is unknown and
// the caller MUST deny.
func (a *Authorizer) ListScope(ctx context.Context, p Principal, action, projectID, kind string) (*Node, error) {
	if !p.valid() {
		return False(), nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("iam: list grants: %w", err)
	}
	return CompileScope(grants, p, action, projectID, kind, a.schema), nil
}

// ListScopes is ListScope for a list that spans projects: one scope per entry
// of projectIDs, compiled from a single fetch of the principal's grants. An
// error means the scopes are unknown and the caller MUST deny.
func (a *Authorizer) ListScopes(ctx context.Context, p Principal, action string, projectIDs []string, kind string) (map[string]*Node, error) {
	out := make(map[string]*Node, len(projectIDs))
	if !p.valid() {
		for _, id := range projectIDs {
			out[id] = False()
		}
		return out, nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("iam: list grants: %w", err)
	}
	for _, id := range projectIDs {
		out[id] = CompileScope(grants, p, action, id, kind, a.schema)
	}
	return out, nil
}

// EffectiveActions returns the registered actions the principal may perform
// on project/<projectID>, or, when projectID is "", the platform actions
// (those with a PlatformRootFor) on their platform root resource.
func (a *Authorizer) EffectiveActions(ctx context.Context, p Principal, projectID string) ([]string, error) {
	if !p.valid() {
		return nil, nil
	}
	grants, err := a.store.ListGrants(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("iam: list grants: %w", err)
	}
	var out []string
	for _, action := range a.reg.Actions() {
		resource := "project/" + projectID
		if projectID == "" {
			if resource = PlatformRootFor(action); resource == "" {
				continue
			}
		}
		r, err := a.evaluate(ctx, grants, p, action, resource, nil)
		if err != nil {
			return nil, err
		}
		if r.Allowed || (projectID != "" && possiblyAllowedOnChild(grants, action, projectID)) {
			out = append(out, action)
		}
	}
	return out, nil
}

// PlatformRootFor maps an action to the platform-level resource a
// collection-level check of it targets, or "" when the action is not a
// platform action (project-scoped domains, plugin actions).
func PlatformRootFor(action string) string {
	domain, _, _ := strings.Cut(action, ":")
	switch domain {
	case "users":
		return "user/*"
	case "roles":
		return "role/*"
	case "plugins":
		return "plugin/*"
	case "settings":
		return "settings"
	case "settings.sso":
		return "sso"
	case "agents":
		return "agent/*"
	case "projects":
		return "project"
	}
	return ""
}

var projectChildKinds = []string{"task", "sprint", "doc", "view", "workflow", "annotation", "agent", "environment", "conversation", "role", "plugin"}

func possiblyAllowedOnChild(grants []Grant, action, projectID string) bool {
	for _, k := range projectChildKinds {
		if PossiblyAllowed(grants, action, "project/"+projectID+"/"+k+"/*") {
			return true
		}
	}
	return false
}

// PossiblyAllowed reports whether some Allow statement matches action and
// resource IGNORING its conditions (a conditional Allow counts as possible),
// while honoring only UNCONDITIONAL Deny statements. Project-scoped grants
// apply only inside their project. Pass a placeholder id ("*") for "some
// resource of this kind"; an Allow on a specific child id under the wildcard
// pattern (e.g., "project/P/role/R1" when checking "project/P/role/*") now
// counts as possibly allowed. Used to decide whether to show UI for an action,
// never to authorize.
func PossiblyAllowed(grants []Grant, action, resource string) bool {
	possible := false
	for _, g := range grants {
		if g.Policy == nil || (g.ProjectID != "" && !inProject(g.ProjectID, resource)) {
			continue
		}
		for _, st := range g.Policy.Statements {
			if !anyMatch(st.Actions, action, MatchAction) {
				continue
			}
			// Check if any policy resource matches the query resource
			matched := false
			for _, policyRes := range st.Resources {
				if MatchResource(policyRes, resource) {
					matched = true
					break
				}
				// For hint purposes: if checking a wildcard and the policy names
				// a specific child under it, count as possible.
				if MatchesUnderWildcard(policyRes, resource) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if st.Effect == EffectDeny && len(st.Conditions) == 0 {
				return false
			}
			if st.Effect == EffectAllow {
				possible = true
			}
		}
	}
	return possible
}
