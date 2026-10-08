package handler

import (
	"testing"

	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
)

func TestRouteMiddlewares_NilVsEmpty(t *testing.T) {
	h := &PluginHandler{}

	t.Run("nil middlewares uses default policy", func(t *testing.T) {
		route := &plugindom.PluginRoute{Public: false, Middlewares: nil}
		got := h.routeMiddlewares(route)
		if len(got) == 0 {
			t.Fatalf("expected default middleware chain, got empty")
		}
		names := make([]string, len(got))
		for i, mw := range got {
			names[i] = mw.Name
		}
		if len(names) != 2 || names[0] != "authn" || names[1] != "requireFreshPassword" {
			t.Fatalf("default policy must be exactly [authn requireFreshPassword] (authenticated-only, no action check), got %v", names)
		}
	})

	t.Run("explicit empty middlewares disables defaults", func(t *testing.T) {
		route := &plugindom.PluginRoute{Public: false, Middlewares: []plugindom.PluginRouteMiddleware{}}
		got := h.routeMiddlewares(route)
		if got == nil {
			t.Fatalf("expected explicit empty slice, got nil")
		}
		if len(got) != 0 {
			t.Fatalf("expected no middlewares, got %d", len(got))
		}
	})

	// A nil *route* (no manifest entry matched this request's method+path at
	// all) must NOT hit the authenticated-only default meant for a declared-but-
	// unprotected route: that default started requiring auth, which would
	// make an unmatched path 401 instead of reaching the plugin's own WASM
	// router to 404 it — i.e. you'd need to log in just to learn a path
	// doesn't exist. See TestE2EPluginRuntime_APICall_UnmatchedPath_PluginReturns404.
	t.Run("nil route (unmatched path) applies no middleware", func(t *testing.T) {
		got := h.routeMiddlewares(nil)
		if got != nil {
			t.Fatalf("expected no middleware for an unmatched path, got %#v", got)
		}
	})
}

func TestMatchPluginRoute_PrefersMostSpecificPattern(t *testing.T) {
	routes := []plugindom.PluginRoute{
		{Method: "GET", Path: "/items/:id"},
		{Method: "GET", Path: "/items/new"},
		{Method: "GET", Path: "/items/*rest"},
	}

	got, _ := matchPluginRoute(routes, "GET", "/items/new")
	if got == nil {
		t.Fatalf("expected route match, got nil")
	}
	if got.Path != "/items/new" {
		t.Fatalf("expected static path match, got %q", got.Path)
	}
}

func TestMatchPluginRoute_KeepsManifestOrderOnSpecificityTie(t *testing.T) {
	routes := []plugindom.PluginRoute{
		{Method: "GET", Path: "/items/:id"},
		{Method: "GET", Path: "/items/:name"},
	}

	got, _ := matchPluginRoute(routes, "GET", "/items/42")
	if got == nil {
		t.Fatalf("expected route match, got nil")
	}
	if got.Path != "/items/:id" {
		t.Fatalf("expected first route on tie, got %q", got.Path)
	}
}
