package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Paca-AI/api/internal/apierr"
	"github.com/Paca-AI/api/internal/platform/authz/iam"
)

// FileAreaDocs is the area of everything in a project's Documentation
// subtree: its folders, its documents and the files inside it.
const FileAreaDocs = "docs"

// FileAreaLookup reports the area of a file manager node (folder, file or
// document) by id, and whether the node is a document. An unknown id has no
// area.
type FileAreaLookup interface {
	FileNodeArea(ctx context.Context, id uuid.UUID) (area string, doc bool, err error)
}

// FileTarget names the node a file manager request acts on or into: the node
// in the URL, a parent folder in the query or body, an upload's file id.
// ok is false when the request names none (nothing to authorize for it).
type FileTarget func(r *http.Request) (id uuid.UUID, ok bool, err error)

// FileURLNode is the node whose id is the URL parameter param.
func FileURLNode(param string) FileTarget {
	return func(r *http.Request) (uuid.UUID, bool, error) {
		id, err := uuid.Parse(chi.URLParam(r, param))
		return id, err == nil, nil
	}
}

// FileQueryParent is the folder a listing names in its parent_id query
// parameter; a listing with none is the project root.
func FileQueryParent(r *http.Request) (uuid.UUID, bool, error) {
	id, err := uuid.Parse(r.URL.Query().Get("parent_id"))
	return id, err == nil, nil
}

// FileBodyField is the node a request body names in the JSON field key
// (parent_id of a create, upload or move; file_id of an upload completion).
// A body it cannot read, or a field that is not a uuid, is left to the
// handler's own 400; data after the body's JSON value is refused, since a
// handler reading only the first value would act on a node this gate never saw.
func FileBodyField(key string) FileTarget {
	return func(r *http.Request) (uuid.UUID, bool, error) {
		buf, err := peekBodyLimit(r, maxAttrPeekBody)
		if err != nil {
			return uuid.Nil, false, err
		}
		var body map[string]json.RawMessage
		switch err := decodeSingleJSON(bytes.NewReader(buf), &body); {
		case errors.Is(err, errTrailingJSON):
			return uuid.Nil, false, apierr.New(apierr.CodeBadRequest, err.Error())
		case err != nil:
			return uuid.Nil, false, nil
		}
		if body, err = foldBodyKeys(body); err != nil {
			return uuid.Nil, false, err
		}
		raw, present := body[jsonFoldKey(key)]
		if !present || string(bytes.TrimSpace(raw)) == "null" {
			return uuid.Nil, false, nil
		}
		var id uuid.UUID
		if err := json.Unmarshal(raw, &id); err != nil {
			return uuid.Nil, false, nil
		}
		return id, true, nil
	}
}

// RequireFileDocsArea adds the docs action wherever a file manager request
// touches the Documentation subtree. The file routes already require
// files:read / files:write on the project; a node in that subtree is
// documentation content, so the caller also needs the docs action (docs:read to
// look, docs:write to change) on project/<projectParam>, or on the document
// itself when the node is one. Every target is
// checked, so moving a file out of or into Documentation needs the action for
// both ends. A request whose targets are all elsewhere is not affected. Declare
// it after the route's files gate.
func RequireFileDocsArea(a *iam.Authorizer, lookup FileAreaLookup, action iam.Action, projectParam string, targets ...FileTarget) func(http.Handler) http.Handler {
	checks := make([]ActionCheck, len(targets))
	for i, target := range targets {
		checks[i] = ActionCheck{Action: action, Resource: func(r *http.Request) (string, error) {
			id, ok, err := target(r)
			if err != nil {
				return "", err
			}
			if !ok {
				return "", ErrNoResource
			}
			if lookup == nil {
				return "", apierr.New(apierr.CodeInternalError, "authorization not configured")
			}
			area, doc, err := lookup.FileNodeArea(r.Context(), id)
			if err != nil {
				return "", err
			}
			if area != FileAreaDocs {
				return "", ErrNoResource
			}
			pid, err := urlUUID(r, projectParam, "project")
			if err != nil {
				return "", err
			}
			// A document is authorized as that document, so a Deny on it or a
			// role limited to some folders applies here as on the docs routes.
			if doc {
				return "project/" + pid + "/doc/" + id.String(), nil
			}
			return "project/" + pid, nil
		}}
	}
	return RequireActions(a, checks...)
}
