package integration_test

import (
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// Legacy permission vocabulary, kept ONLY for the migration tests: the
// runtime is IAM-native (iam.Action) and the one-time conversion of stored
// roles lives in migration 000064's SQL helpers. These Go mirrors let the
// golden test assert that the SQL helpers map exactly as specified.

// legacyBuiltinKeys lists every built-in legacy permission key (no
// wildcards), as the deleted internal/platform/authz/permissions.go defined
// them — the same list as 000064's pg_temp.legacy_builtin_keys().
var legacyBuiltinKeys = []string{
	"users.read", "users.write", "users.delete",
	"global_roles.read", "global_roles.write", "global_roles.assign",
	"projects.read", "projects.write", "projects.create", "projects.delete",
	"project.members.read", "project.members.write",
	"project.roles.read", "project.roles.write",
	"project.activities.read", "project.export",
	"tasks.read", "tasks.write",
	"project.settings.task_types.write", "project.settings.task_statuses.write", "project.settings.custom_fields.write",
	"sprints.read", "sprints.write", "views.read", "views.write", "docs.read", "docs.write",
	"agents.read", "agents.write", "conversations.read", "conversations.write",
	"environments.read", "environments.write", "environments.connect",
	"workflows.read", "workflows.write",
	"annotations.read", "annotations.write", "annotations.resolve",
	"settings.write", "settings.sso.write", "plugins.read", "plugins.write",
}

// legacyKeyToAction converts a legacy dotted permission key to an IAM
// action: "*" and keys without a dot are unchanged; otherwise the last "."
// becomes ":". The legacy domains global_roles and project.roles both map to
// the "roles" domain. Mirrors pg_temp.legacy_key_to_action in 000064 (and
// the per-key rule of 000065's plugin manifest conversion).
func legacyKeyToAction(key string) string {
	if key == "*" {
		return key
	}
	for _, d := range []string{"global_roles.", "project.roles."} {
		if rest, ok := strings.CutPrefix(key, d); ok && !strings.Contains(rest, ".") {
			return "roles:" + rest
		}
	}
	i := strings.LastIndex(key, ".")
	if i < 0 {
		return key
	}
	return key[:i] + ":" + key[i+1:]
}

// The legacy built-in keys map exactly onto iam.BuiltinActions — the
// vocabulary migration 000064 converted roles into.
func TestLegacyBuiltinKeysMapOntoBuiltinActions(t *testing.T) {
	var got, want []string
	for _, k := range legacyBuiltinKeys {
		if a := legacyKeyToAction(k); !slices.Contains(got, a) {
			got = append(got, a)
		}
	}
	for _, a := range iam.BuiltinActions() {
		want = append(want, string(a))
	}
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("legacy keys map to %v,\nBuiltinActions is %v", got, want)
	}
}
