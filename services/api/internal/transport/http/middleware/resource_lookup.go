package middleware

import (
	"strconv"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	agentdom "github.com/Paca-AI/api/internal/domain/agent"
	taskdom "github.com/Paca-AI/api/internal/domain/task"
)

// maxPeekBody bounds how much of a request body a resolver reads to find the
// environment a chat will use.
const maxPeekBody = 1 << 20

// AgentEnvironmentLookup returns an agent's default environment, scoped to
// the project: agentdom.ErrAgentNotFound when the agent is not visible in it,
// nil when it has no default.
type AgentEnvironmentLookup interface {
	DefaultEnvironmentID(ctx context.Context, projectID, agentID uuid.UUID) (*uuid.UUID, error)
}

// SessionEnvironmentLookup returns the environment a chat session runs in
// (that of its latest conversation), scoped to the project and agent:
// agentdom.ErrChatSessionNotFound when the session is not that agent's in
// that project, nil when it has no environment.
type SessionEnvironmentLookup interface {
	SessionEnvironmentID(ctx context.Context, projectID, agentID, sessionID uuid.UUID) (*uuid.UUID, error)
}

// ChatEnvironmentResource names the environment a new chat will use:
// "project/<P>/environment/<E>" with E the request body's environment_id or,
// when the body names none, the default environment of the agent in the URL.
// With no environment at all it returns ErrNoResource.
//
// The body is read (bounded) and restored for the handler. It is decoded the
// way the handler decodes it — first JSON value only, same field type — so
// the two can never disagree on which environment was named. Syntactically
// invalid JSON is left to the handler's 400 (no environment is guessed); a
// body environment_id that is not a UUID is a 400 here.
func ChatEnvironmentResource(lookup AgentEnvironmentLookup, projectParam, agentParam string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		projectID, err := urlUUID(r, projectParam, "project")
		if err != nil {
			return "", err
		}
		agentID, err := urlUUID(r, agentParam, "agent")
		if err != nil {
			return "", err
		}
		envID, err := bodyEnvironmentID(r)
		if err != nil {
			return "", err
		}
		if envID == nil {
			if lookup == nil {
				return "", apierr.New(apierr.CodeInternalError, "authorization not configured")
			}
			envID, err = lookup.DefaultEnvironmentID(r.Context(), uuid.MustParse(projectID), uuid.MustParse(agentID))
			if err != nil {
				return "", err
			}
		}
		if envID == nil {
			return "", ErrNoResource
		}
		return "project/" + projectID + "/environment/" + envID.String(), nil
	}
}

// bodyEnvironmentID peeks the body's environment_id and restores the body.
func bodyEnvironmentID(r *http.Request) (*uuid.UUID, error) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxPeekBody+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, apierr.New(apierr.CodeBadRequest, "could not read request body")
	}
	if len(buf) > maxPeekBody {
		return nil, apierr.New(apierr.CodeBadRequest, "request body too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(buf))

	var req struct {
		EnvironmentID *uuid.UUID `json:"environment_id"`
	}
	err = json.NewDecoder(bytes.NewReader(buf)).Decode(&req)
	var syntax *json.SyntaxError
	switch {
	case err == nil:
		return req.EnvironmentID, nil
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &syntax):
		return nil, nil // not JSON the handler can use either: its 400
	default:
		return nil, apierr.New(apierr.CodeBadRequest, "invalid environment_id")
	}
}

// SessionEnvironmentResource names the environment of the chat session in
// the URL: "project/<P>/environment/<E>", or ErrNoResource when the session
// has none. The lookup is scoped by the project, agent and session ids from
// the URL (not found -> the lookup's not-found error, a 404).
func SessionEnvironmentResource(lookup SessionEnvironmentLookup, projectParam, agentParam, sessionParam string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		projectID, err := urlUUID(r, projectParam, "project")
		if err != nil {
			return "", err
		}
		agentID, err := urlUUID(r, agentParam, "agent")
		if err != nil {
			return "", err
		}
		sessionID, err := urlUUID(r, sessionParam, "session")
		if err != nil {
			return "", err
		}
		if lookup == nil {
			return "", apierr.New(apierr.CodeInternalError, "authorization not configured")
		}
		envID, err := lookup.SessionEnvironmentID(r.Context(), uuid.MustParse(projectID), uuid.MustParse(agentID), uuid.MustParse(sessionID))
		if err != nil {
			return "", err
		}
		if envID == nil {
			return "", ErrNoResource
		}
		return "project/" + projectID + "/environment/" + envID.String(), nil
	}
}

// TaskNumberLookup finds the task a project-scoped task number names.
type TaskNumberLookup interface {
	FindTaskByNumber(ctx context.Context, projectID uuid.UUID, taskNumber int64) (*taskdom.Task, error)
}

// TaskByNumberResource names the task in the URL by its project-scoped number:
// "project/<P>/task/<T>". A number that names no task, or one that is not a
// number, falls back to the project itself ("project/<P>"), so the caller still
// needs project-level access before the handler answers 404/400 and nothing is
// revealed to someone without it.
func TaskByNumberResource(lookup TaskNumberLookup, projectParam, numberParam string) ResourceResolver {
	return func(r *http.Request) (string, error) {
		projectID, err := urlUUID(r, projectParam, "project")
		if err != nil {
			return "", err
		}
		n, err := strconv.ParseInt(chi.URLParam(r, numberParam), 10, 64)
		if err != nil || n < 1 {
			return "project/" + projectID, nil
		}
		if lookup == nil {
			return "", apierr.New(apierr.CodeInternalError, "authorization not configured")
		}
		t, err := lookup.FindTaskByNumber(r.Context(), uuid.MustParse(projectID), n)
		if errors.Is(err, taskdom.ErrTaskNotFound) {
			return "project/" + projectID, nil
		}
		if err != nil {
			return "", err
		}
		return "project/" + projectID + "/task/" + t.ID.String(), nil
	}
}

// AgentRepoLookups implements both lookups over the agent repository.
type AgentRepoLookups struct{ Repo agentdom.Repository }

// DefaultEnvironmentID implements AgentEnvironmentLookup.
func (l AgentRepoLookups) DefaultEnvironmentID(ctx context.Context, projectID, agentID uuid.UUID) (*uuid.UUID, error) {
	a, err := l.Repo.FindVisibleAgentInProject(ctx, projectID, agentID)
	if err != nil {
		return nil, err
	}
	return a.DefaultEnvironmentID, nil
}

// SessionEnvironmentID implements SessionEnvironmentLookup.
func (l AgentRepoLookups) SessionEnvironmentID(ctx context.Context, projectID, agentID, sessionID uuid.UUID) (*uuid.UUID, error) {
	s, err := l.Repo.FindChatSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if s.ProjectID != projectID || s.AgentID != agentID {
		return nil, agentdom.ErrChatSessionNotFound
	}
	latest, err := l.Repo.FindLatestConversationByChatSession(ctx, sessionID)
	if err != nil || latest == nil {
		return nil, err
	}
	return latest.EnvironmentID, nil
}
