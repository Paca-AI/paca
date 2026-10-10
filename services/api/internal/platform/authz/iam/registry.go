package iam

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Registry is the catalogue of known actions. It is safe for concurrent use
// (plugins register actions at runtime).
type Registry struct {
	mu      sync.RWMutex
	actions map[string]struct{}
	domains map[string]struct{}
	// plugins holds the actions each plugin declares, keyed by plugin id, so a
	// plugin's set can be replaced or removed as a whole.
	plugins map[string][]string
	// owners maps each plugin action back to the plugin that declares it.
	owners map[string]string
}

// NewRegistry returns a registry seeded with BuiltinActions.
func NewRegistry() *Registry {
	r := &Registry{actions: map[string]struct{}{}, domains: map[string]struct{}{}, plugins: map[string][]string{}, owners: map[string]string{}}
	for _, a := range BuiltinActions() {
		if err := r.Register(string(a)); err != nil {
			panic(err) // builtin list is static; a bad entry is a programming error
		}
	}
	return r
}

// Register adds an action of the form "domain:verb" (domain may contain
// dots, e.g. "plugin.jev:sync"). It rejects anything containing "*" or
// whitespace, or not having exactly one ":" with a non-empty domain and verb.
func (r *Registry) Register(action string) error {
	domain, err := validateAction(action)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions[action] = struct{}{}
	r.domains[domain] = struct{}{}
	return nil
}

// validateAction checks the "domain:verb" form and returns the domain.
func validateAction(action string) (string, error) {
	if strings.Contains(action, "*") {
		return "", fmt.Errorf("iam: action %q must not contain a wildcard", action)
	}
	if strings.IndexFunc(action, unicode.IsSpace) >= 0 {
		return "", fmt.Errorf("iam: action %q must not contain whitespace", action)
	}
	if strings.Count(action, ":") != 1 {
		return "", fmt.Errorf("iam: action %q must have the form domain:verb", action)
	}
	domain, verb, _ := strings.Cut(action, ":")
	if domain == "" || verb == "" {
		return "", fmt.Errorf("iam: action %q must have a non-empty domain and verb", action)
	}
	return domain, nil
}

// SetPluginActions makes actions the complete set the plugin owner declares,
// replacing what it declared before. Every action must be valid and neither a
// built-in nor another plugin's; on error nothing changes. A role editor can
// then grant them and a policy naming them validates.
func (r *Registry) SetPluginActions(owner string, actions []string) error {
	for _, a := range actions {
		if _, err := validateAction(a); err != nil {
			return err
		}
		if IsBuiltinAction(a) {
			return fmt.Errorf("iam: plugin %s cannot redeclare built-in action %q", owner, a)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for other, as := range r.plugins {
		if other == owner {
			continue
		}
		for _, theirs := range as {
			for _, mine := range actions {
				if theirs == mine {
					return fmt.Errorf("iam: action %q is already declared by plugin %s", mine, other)
				}
			}
		}
	}
	r.dropPluginLocked(owner)
	r.plugins[owner] = append([]string(nil), actions...)
	for _, a := range actions {
		r.actions[a] = struct{}{}
		r.owners[a] = owner
	}
	r.recomputeDomainsLocked()
	return nil
}

// RemovePluginActions forgets everything the plugin owner declared.
func (r *Registry) RemovePluginActions(owner string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropPluginLocked(owner)
	r.recomputeDomainsLocked()
}

// PluginOwner returns the id of the plugin that declares action, or "" for a
// built-in or unknown action.
func (r *Registry) PluginOwner(action string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.owners[action]
}

func (r *Registry) dropPluginLocked(owner string) {
	for _, a := range r.plugins[owner] {
		delete(r.actions, a)
		delete(r.owners, a)
	}
	delete(r.plugins, owner)
}

func (r *Registry) recomputeDomainsLocked() {
	r.domains = map[string]struct{}{}
	for a := range r.actions {
		d, _, _ := strings.Cut(a, ":")
		r.domains[d] = struct{}{}
	}
}

// HasAction reports whether a is a registered action, "*", or "domain:*"
// for a domain with at least one registered action.
func (r *Registry) HasAction(a string) bool {
	if a == "*" {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.actions[a]; ok {
		return true
	}
	if d, ok := strings.CutSuffix(a, ":*"); ok {
		_, known := r.domains[d]
		return known
	}
	return false
}

// Actions returns a sorted copy of all registered actions.
func (r *Registry) Actions() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.actions))
	for a := range r.actions {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}
