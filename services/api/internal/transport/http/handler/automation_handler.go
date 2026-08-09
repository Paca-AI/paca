package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	automationdom "github.com/Paca-AI/api/internal/domain/automation"
	plugindom "github.com/Paca-AI/api/internal/domain/plugin"
	"github.com/Paca-AI/api/internal/transport/http/dto"
	"github.com/Paca-AI/api/internal/transport/http/middleware"
	"github.com/Paca-AI/api/internal/transport/http/presenter"
)

// pluginAutomationNodeLister exposes the plugin runtime's registry of
// plugin-contributed automation node types, for the canvas node palette.
type pluginAutomationNodeLister interface {
	AutomationNodeTypes() (triggers, conditions, actions []plugindom.AutomationNodeManifest)
}

// AutomationHandler handles automation-graph management endpoints.
type AutomationHandler struct {
	svc           automationdom.Service
	pluginRuntime pluginAutomationNodeLister
}

// NewAutomationHandler returns an AutomationHandler wired to the automation service.
func NewAutomationHandler(svc automationdom.Service) *AutomationHandler {
	return &AutomationHandler{svc: svc}
}

// WithPluginRuntime attaches the plugin runtime so ListPluginNodeTypes can
// serve the canvas node palette's plugin-contributed entries.
func (h *AutomationHandler) WithPluginRuntime(rt pluginAutomationNodeLister) *AutomationHandler {
	h.pluginRuntime = rt
	return h
}

// ListPluginNodeTypes handles GET /projects/:projectId/automation-plugin-node-types.
func (h *AutomationHandler) ListPluginNodeTypes(w http.ResponseWriter, r *http.Request) {
	if h.pluginRuntime == nil {
		presenter.OK(w, r, map[string]any{"triggers": []any{}, "conditions": []any{}, "actions": []any{}})
		return
	}
	triggers, conditions, actions := h.pluginRuntime.AutomationNodeTypes()
	presenter.OK(w, r, map[string]any{
		"triggers":   triggers,
		"conditions": conditions,
		"actions":    actions,
	})
}

// --- Automations ----------------------------------------------------------

// ListAutomations handles GET /projects/:projectId/automations.
func (h *AutomationHandler) ListAutomations(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var status *automationdom.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := automationdom.Status(raw)
		if !automationdom.ValidStatuses[s] {
			presenter.Error(w, r, apierr.New(apierr.CodeBadRequest, "invalid status filter"))
			return
		}
		status = &s
	}

	automations, err := h.svc.ListAutomations(r.Context(), projectID, status)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AutomationResponse, 0, len(automations))
	for _, a := range automations {
		resp = append(resp, dto.NewAutomationResponse(a))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// CreateAutomation handles POST /projects/:projectId/automations.
func (h *AutomationHandler) CreateAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var req dto.CreateAutomationRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}

	var createdBy *uuid.UUID
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok && actorID != uuid.Nil {
		createdBy = &actorID
	}
	var agentID *uuid.UUID
	if id, ok := middleware.AgentIDFromContext(r.Context()); ok && id != uuid.Nil {
		agentID = &id
	}

	a, err := h.svc.CreateAutomation(r.Context(), automationdom.CreateAutomationInput{
		ProjectID:   projectID,
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   createdBy,
		AgentID:     agentID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.NewAutomationResponse(a))
}

// GetAutomation handles GET /projects/:projectId/automations/:automationId.
func (h *AutomationHandler) GetAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	graph, err := h.svc.GetAutomation(r.Context(), projectID, automationID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.NewAutomationGraphResponse(graph))
}

// UpdateAutomation handles PATCH /projects/:projectId/automations/:automationId.
func (h *AutomationHandler) UpdateAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateAutomationRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	a, err := h.svc.UpdateAutomation(r.Context(), projectID, automationID, automationdom.UpdateAutomationInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.NewAutomationResponse(a))
}

// DeleteAutomation handles DELETE /projects/:projectId/automations/:automationId.
func (h *AutomationHandler) DeleteAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.DeleteAutomation(r.Context(), projectID, automationID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// ActivateAutomation handles POST /projects/:projectId/automations/:automationId/activate.
func (h *AutomationHandler) ActivateAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.Activate(r.Context(), projectID, automationID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.NewAutomationResponse(a))
}

// DeactivateAutomation handles POST /projects/:projectId/automations/:automationId/deactivate.
func (h *AutomationHandler) DeactivateAutomation(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	a, err := h.svc.Deactivate(r.Context(), projectID, automationID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.NewAutomationResponse(a))
}

// --- Nodes ------------------------------------------------------------------

// AddAutomationNode handles POST /projects/:projectId/automations/:automationId/nodes.
func (h *AutomationHandler) AddAutomationNode(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddAutomationNodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	n, err := h.svc.AddNode(r.Context(), projectID, automationID, automationdom.AddNodeInput{
		Kind:   automationdom.Kind(req.Kind),
		Type:   req.Type,
		Config: req.Config,
		PosX:   req.PosX,
		PosY:   req.PosY,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.NewAutomationNodeResponse(n))
}

// UpdateAutomationNode handles PATCH /projects/:projectId/automations/:automationId/nodes/:nodeId.
func (h *AutomationHandler) UpdateAutomationNode(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	nodeID, err := parseAutomationNodeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.UpdateAutomationNodeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	n, err := h.svc.UpdateNode(r.Context(), projectID, automationID, nodeID, automationdom.UpdateNodeInput{
		Config: req.Config,
		PosX:   req.PosX,
		PosY:   req.PosY,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.OK(w, r, dto.NewAutomationNodeResponse(n))
}

// RemoveAutomationNode handles DELETE /projects/:projectId/automations/:automationId/nodes/:nodeId.
func (h *AutomationHandler) RemoveAutomationNode(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	nodeID, err := parseAutomationNodeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.RemoveNode(r.Context(), projectID, automationID, nodeID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// GenerateWebhookToken handles
// POST /projects/:projectId/automations/:automationId/nodes/:nodeId/webhook-token.
// (Re)generates the secret token for an api_trigger node, revoking whichever
// token preceded it. The raw token is returned only in this response — it's
// never persisted or retrievable again afterward.
func (h *AutomationHandler) GenerateWebhookToken(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	nodeID, err := parseAutomationNodeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}

	var actor automationdom.WebhookTokenActor
	if actorID, ok := middleware.ActorIDFromContext(r.Context()); ok && actorID != uuid.Nil {
		actor.UserID = &actorID
	}
	if agentID, ok := middleware.AgentIDFromContext(r.Context()); ok && agentID != uuid.Nil {
		actor.AgentID = &agentID
	}

	tok, rawToken, err := h.svc.GenerateWebhookToken(r.Context(), projectID, automationID, nodeID, actor)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.CreateWebhookTokenResponse{
		WebhookTokenResponse: dto.NewWebhookTokenResponse(tok),
		Token:                rawToken,
	})
}

// --- Edges ------------------------------------------------------------------

// AddAutomationEdge handles POST /projects/:projectId/automations/:automationId/edges.
func (h *AutomationHandler) AddAutomationEdge(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	var req dto.AddAutomationEdgeRequest
	if !middleware.BindJSON(w, r, &req) {
		return
	}
	e, err := h.svc.AddEdge(r.Context(), projectID, automationID, automationdom.AddEdgeInput{
		SourceNodeID: req.SourceNodeID,
		SourceHandle: req.SourceHandle,
		TargetNodeID: req.TargetNodeID,
	})
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Created(w, r, dto.NewAutomationEdgeResponse(e))
}

// RemoveAutomationEdge handles DELETE /projects/:projectId/automations/:automationId/edges/:edgeId.
func (h *AutomationHandler) RemoveAutomationEdge(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	edgeID, err := parseAutomationEdgeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	if err := h.svc.RemoveEdge(r.Context(), projectID, automationID, edgeID); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.NoContent(w)
}

// --- Runs -------------------------------------------------------------------

// ListAutomationRuns handles GET /projects/:projectId/automations/:automationId/runs.
func (h *AutomationHandler) ListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	runs, err := h.svc.ListRuns(r.Context(), projectID, automationID, limit)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AutomationRunResponse, 0, len(runs))
	for _, run := range runs {
		resp = append(resp, dto.NewAutomationRunResponse(run))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// ListAutomationRunSteps handles GET /projects/:projectId/automations/:automationId/runs/:runId/steps.
func (h *AutomationHandler) ListAutomationRunSteps(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	automationID, err := parseAutomationID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	runID, err := parseAutomationRunID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	steps, err := h.svc.ListRunSteps(r.Context(), projectID, automationID, runID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.AutomationRunStepResponse, 0, len(steps))
	for _, s := range steps {
		resp = append(resp, dto.NewAutomationRunStepResponse(s))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// --- Dependency map -----------------------------------------------------------

// GetAutomationDependencyMap handles GET /projects/:projectId/automation-dependency-map.
func (h *AutomationHandler) GetAutomationDependencyMap(w http.ResponseWriter, r *http.Request) {
	projectID, err := parseProjectID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	entries, err := h.svc.DependencyMap(r.Context(), projectID)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	resp := make([]dto.DependencyMapEntryResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, dto.NewDependencyMapEntryResponse(e))
	}
	presenter.OK(w, r, map[string]any{"items": resp})
}

// --- Webhook receiver ---------------------------------------------------------

// ReceiveWebhook handles POST /webhooks/automations/:nodeId. Unlike every
// other automation endpoint, this one is deliberately registered outside
// the authenticated /projects/:projectId route group — the caller is an
// external system, not a logged-in user, so it carries its own secret
// token (X-Webhook-Token header) instead of a session/API key, verified
// entirely inside automationdom.Service.HandleWebhookTrigger. Responds 202
// as soon as the token is verified and the run is durably queued, without
// waiting for the graph walk to finish (see events.StreamAutomationExternalTriggers).
func (h *AutomationHandler) ReceiveWebhook(w http.ResponseWriter, r *http.Request) {
	nodeID, err := parseAutomationNodeID(r)
	if err != nil {
		presenter.Error(w, r, err)
		return
	}
	token := r.Header.Get("X-Webhook-Token")
	if token == "" {
		presenter.Error(w, r, automationdom.ErrWebhookTokenInvalid)
		return
	}
	if err := h.svc.HandleWebhookTrigger(r.Context(), nodeID, token); err != nil {
		presenter.Error(w, r, err)
		return
	}
	presenter.Accepted(w, r, map[string]any{"accepted": true})
}

// --- helpers ----------------------------------------------------------------

func parseAutomationID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "automationId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid automation id")
	}
	return id, nil
}

func parseAutomationNodeID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "nodeId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid node id")
	}
	return id, nil
}

func parseAutomationEdgeID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "edgeId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid edge id")
	}
	return id, nil
}

func parseAutomationRunID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		return uuid.Nil, apierr.New(apierr.CodeBadRequest, "invalid run id")
	}
	return id, nil
}
