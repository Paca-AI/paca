package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestAgentTurnPolicyRejectsAgentMutation(t *testing.T) {
	nextCalled := false
	handler := EnforceAgentTurnReadOnly()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/api/v1/projects/p/tasks/t", nil)
	request.Header.Set("X-Agent-Turn-ID", uuid.NewString())
	request = request.WithContext(WithAgentID(request.Context(), uuid.New()))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || nextCalled {
		t.Fatalf("status=%d nextCalled=%v", response.Code, nextCalled)
	}
}

func TestAgentTurnPolicyAllowsAgentRead(t *testing.T) {
	nextCalled := false
	handler := EnforceAgentTurnReadOnly()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/projects/p/tasks/t", nil)
	request.Header.Set("X-Agent-Turn-ID", uuid.NewString())
	request = request.WithContext(WithAgentID(request.Context(), uuid.New()))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("status=%d nextCalled=%v", response.Code, nextCalled)
	}
}

func TestAgentTurnPolicyDoesNotTreatHumanHeaderAsTurnCredential(t *testing.T) {
	nextCalled := false
	handler := EnforceAgentTurnReadOnly()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/projects/p/chat-sessions", nil)
	request.Header.Set("X-Agent-Turn-ID", uuid.NewString())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("status=%d nextCalled=%v", response.Code, nextCalled)
	}
}
