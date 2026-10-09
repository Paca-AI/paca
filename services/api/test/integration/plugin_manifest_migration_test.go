package integration_test

import (
	"encoding/json"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/platform/database"
	"github.com/Paca-AI/api/migrations"
)

const pluginManifestMigrationFile = "000064_iam_roles.sql"
const pluginManifestHelpersMarker = "plugin-manifest-helpers"

// goConvertManifest is the specified conversion in Go: every
// requirePermissions middleware becomes requireActions, each key mapped with
// legacyKeyToAction (the same rule migration 000064 used for role keys);
// everything else is untouched.
func goConvertManifest(t *testing.T, raw string) any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	convertPermissionKeys(m)
	backend, _ := m["backend"].(map[string]any)
	routes, _ := backend["routes"].([]any)
	for _, r := range routes {
		route, _ := r.(map[string]any)
		mws, _ := route["middlewares"].([]any)
		for i, x := range mws {
			mw, _ := x.(map[string]any)
			if name, _ := mw["name"].(string); strings.ToLower(name) != "requirepermissions" {
				continue
			}
			actions := []any{}
			perms, _ := mw["permissions"].([]any)
			for _, p := range perms {
				actions = append(actions, legacyKeyToAction(p.(string)))
			}
			delete(mw, "permissions")
			mw["name"] = "requireActions"
			mw["actions"] = actions
			mws[i] = mw
		}
	}
	return m
}

// toAction is legacyKeyToAction, except that a value already in action form
// stays as it is (so the conversion is idempotent).
func toAction(k string) string {
	if strings.Contains(k, ":") {
		return k
	}
	return legacyKeyToAction(k)
}

// convertPermissionKeys converts, in place and anywhere in the document, every
// requiredPermission and every customPermissions[].key with legacyKeyToAction.
func convertPermissionKeys(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			switch k {
			case "requiredPermission":
				if s, ok := child.(string); ok {
					x[k] = toAction(s)
				}
			case "customPermissions":
				list, _ := child.([]any)
				for _, c := range list {
					if cm, ok := c.(map[string]any); ok {
						if key, ok := cm["key"].(string); ok {
							cm["key"] = toAction(key)
						}
					}
				}
			default:
				convertPermissionKeys(child)
			}
		}
	case []any:
		for _, c := range x {
			convertPermissionKeys(c)
		}
	}
}

func TestPluginManifestActionsMigration(t *testing.T) {
	db := newMigrationTestDB(t)
	if err := database.RunMigrationsFS(db, migrationsBefore(t, pluginManifestMigrationFile)); err != nil {
		t.Fatalf("migrate to 000063: %v", err)
	}
	m, err := fs.ReadFile(migrations.FS, pluginManifestMigrationFile)
	if err != nil {
		t.Fatal(err)
	}
	_, pluginSQL, _ := markedBlock(t, string(m), "plugin-manifest-actions")
	pluginSQL = strings.TrimSpace(strings.TrimPrefix(pluginSQL, "-- >>> plugin-manifest-actions\n"))
	pluginSQL = strings.TrimSpace(strings.TrimSuffix(pluginSQL, "-- <<< plugin-manifest-actions"))
	pluginSQL = strings.TrimPrefix(pluginSQL, "-- Folded plugin manifest conversion (previously migration 000065).")
	pluginSQL = strings.TrimSpace(pluginSQL)
	// Persist the fixture manifest in the pre-migration schema before running
	// the selected operation from the consolidated migration.
	manifests := map[string]string{
		"com.paca.time-logging": `{"id":"com.paca.time-logging","version":"1.0.0","backend":{"routes":[
			{"method":"GET","path":"/projects/:projectId/logs","middlewares":[{"name":"authn"},{"name":"requirePermissions","scope":"project","projectParam":"projectId","permissions":["tasks.read","time_logging.manage_all"]}]},
			{"method":"POST","path":"/admin","middlewares":[{"name":"authn"},{"name":"RequirePermissions","scope":"global","permissions":["global_roles.read","project.settings.task_types.write","settings.sso.write"]}]},
			{"method":"GET","path":"/public","public":true},
			{"method":"GET","path":"/open","middlewares":[]},
			{"method":"GET","path":"/default","middlewares":null}
		],"eventSubscriptions":["task.created"]},"customPermissions":[{"key":"time_logging.manage_all","scope":"project"},{"key":"time_logging:approve"}],
			"frontend":{"remoteEntry":"x.js","navItems":[{"label":"Logs","requiredPermission":"time_logging.manage_all"},{"label":"Any"}],
			"extensions":[{"point":"project.settings.tab","registrations":[{"component":"C","requiredPermission":"project.settings.task_types.write"}]}]}}`,
		"com.paca.converted": `{"id":"com.paca.converted","backend":{"routes":[{"method":"GET","path":"/x","middlewares":[{"name":"requireActions","actions":["tasks:read"]}]}]}}`,
		"com.paca.nobackend": `{"id":"com.paca.nobackend","frontend":{"remoteEntry":"x.js"}}`,
		"com.paca.empty":     `{"id":"com.paca.empty","backend":{"routes":[]}}`,
		"com.paca.noperms":   `{"id":"com.paca.noperms","backend":{"routes":[{"method":"GET","path":"/y","middlewares":[{"name":"requirePermissions"}]}]}}`,
	}
	for name, m := range manifests {
		mustExec(t, db, `INSERT INTO plugins (id, name, manifest) VALUES ($1, $2, $3::jsonb)`, uuid.New(), name, m)
	}

	if _, err := db.ExecContext(t.Context(), pluginSQL); err != nil {
		t.Fatalf("apply plugin-manifest conversion: %v", err)
	}
	check := func(round string) {
		for name, m := range manifests {
			var got string
			if err := db.QueryRowContext(t.Context(), `SELECT manifest::text FROM plugins WHERE name = $1`, name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			var gotV any
			if err := json.Unmarshal([]byte(got), &gotV); err != nil {
				t.Fatal(err)
			}
			if want := goConvertManifest(t, m); !reflect.DeepEqual(gotV, want) {
				t.Errorf("%s %s:\n got  %s\n want %v", round, name, got, want)
			}
			if strings.Contains(strings.ToLower(got), "requirepermissions") {
				t.Errorf("%s %s still declares requirePermissions: %s", round, name, got)
			}
		}
	}
	check("first run")

	// Literal expectations, independent of the Go conversion rule above.
	var tl string
	if err := db.QueryRowContext(t.Context(), `SELECT manifest::text FROM plugins WHERE name = 'com.paca.time-logging'`).Scan(&tl); err != nil {
		t.Fatal(err)
	}
	var tlManifest struct {
		Backend struct {
			Routes []struct {
				Middlewares []struct {
					Name    string   `json:"name"`
					Actions []string `json:"actions"`
				} `json:"middlewares"`
			} `json:"routes"`
		} `json:"backend"`
	}
	if err := json.Unmarshal([]byte(tl), &tlManifest); err != nil {
		t.Fatal(err)
	}
	wantActions := [][]string{
		{"tasks:read", "time_logging:manage_all"},
		{"roles:read", "project.settings.task_types:write", "settings.sso:write"},
	}
	for i, want := range wantActions {
		mw := tlManifest.Backend.Routes[i].Middlewares[1]
		if mw.Name != "requireActions" || !reflect.DeepEqual(mw.Actions, want) {
			t.Errorf("time-logging route %d: got %s %v, want requireActions %v", i, mw.Name, mw.Actions, want)
		}
	}

	// Literal expectations for the declared permissions.
	var perms struct {
		Custom []struct {
			Key string `json:"key"`
		} `json:"customPermissions"`
		Frontend struct {
			NavItems []struct {
				Required string `json:"requiredPermission"`
			} `json:"navItems"`
		} `json:"frontend"`
	}
	if err := json.Unmarshal([]byte(tl), &perms); err != nil {
		t.Fatal(err)
	}
	if len(perms.Custom) != 2 || perms.Custom[0].Key != "time_logging:manage_all" || perms.Custom[1].Key != "time_logging:approve" {
		t.Errorf("customPermissions = %+v", perms.Custom)
	}
	if len(perms.Frontend.NavItems) != 2 || perms.Frontend.NavItems[0].Required != "time_logging:manage_all" || perms.Frontend.NavItems[1].Required != "" {
		t.Errorf("navItems = %+v", perms.Frontend.NavItems)
	}
	if !strings.Contains(tl, `"requiredPermission": "project.settings.task_types:write"`) {
		t.Errorf("a nested requiredPermission was not converted: %s", tl)
	}

	// Idempotent: replaying the conversion block changes nothing.
	if _, err := db.ExecContext(t.Context(), pluginSQL); err != nil {
		t.Fatalf("replay plugin-manifest conversion: %v", err)
	}
	check("replay")
}
