// Package e2e contains end-to-end smoke tests for the Paca API service.
// These tests require a running server (or start one inline) and exercise the
// full request flow: login → authorized call → response.
package e2e_test

import (
	"testing"
)

// TestAPIFlow is a placeholder for the login → authorized action smoke test.
// Populate this once the integration environment (Postgres + Redis) is
// available in CI via docker-compose or testcontainers.
func TestAPIFlow(t *testing.T) {
	t.Skip("e2e tests require a running backing services; enable in CI with PACA_E2E=1")
}
