// environment_handlers.go adds agent-runner's internal, server-to-server
// static-environment provisioning endpoints — split out of server.go so
// that file (already covering the ACP bridge WebSocket, its status/
// disconnect endpoints, and the LLM model catalog) doesn't grow
// unbounded. Registration still lands on the one shared mux Server.Routes
// builds (see registerEnvironmentRoutes below, called from there).
//
// Every endpoint here is called only by services/api, over the
// docker-internal/cluster-internal network, guarded by the same
// requireInternalToken check (X-Internal-Token) as
// /agent-bridge/status/* and /agent-bridge/disconnect/*. Contrast with
// terminal.go's browser terminal WebSocket, which is reached directly by a
// browser and therefore can't use that header — it's authenticated by a
// short-lived signed ticket instead.
//
// agent-runner's job here is intentionally thin: services/api's own
// Postgres layer owns the canonical `environments` row lifecycle,
// including the INSERT — these handlers only provision or change the
// backend resource (a Docker container + volume, or a Kubernetes
// Deployment + PersistentVolumeClaim) and report back what was
// created/changed. Nothing in this file touches Postgres.
package acpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// registerEnvironmentRoutes adds the internal environment-provisioning
// endpoints to mux.
func (s *Server) registerEnvironmentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/environments", s.requireInternalToken(s.handleCreateEnvironment))
	mux.HandleFunc("POST /internal/environments/{id}/start", s.requireInternalToken(s.handleStartEnvironment))
	mux.HandleFunc("POST /internal/environments/{id}/stop", s.requireInternalToken(s.handleStopEnvironment))
	mux.HandleFunc("DELETE /internal/environments/{id}", s.requireInternalToken(s.handleDeleteEnvironment))
	mux.HandleFunc("POST /internal/environments/{id}/folders", s.requireInternalToken(s.handleCreateEnvironmentFolder))
	mux.HandleFunc("GET /internal/environments/{id}/browse", s.requireInternalToken(s.handleBrowseEnvironment))
	mux.HandleFunc("POST /internal/environments/{id}/ssh-keys/sync", s.requireInternalToken(s.handleSyncEnvironmentSSHKeys))
	mux.HandleFunc("POST /internal/environments/{id}/port-forwards/assign", s.requireInternalToken(s.handlePortForwardsAssign))
	mux.HandleFunc("POST /internal/environments/{id}/restart-ports", s.requireInternalToken(s.handleRestartEnvironmentPorts))
}

// writeJSON encodes v as the response body with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeJSONError writes {"error": msg} — the error shape every endpoint in
// this file uses on a 4xx/5xx response.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) requireSandboxMgr(w http.ResponseWriter) bool {
	if s.SandboxMgr != nil {
		return true
	}
	writeJSONError(w, http.StatusInternalServerError, "sandbox backend not configured")
	return false
}

// -----------------------------------------------------------------------
// SSH port assignment and port-forward mapping (docs/ai-agent/
// environment-management.md's "Terminal / SSH Access" and "Port
// Forwarding" sections) — deciding which host port maps to which
// container port. Nothing in this file relays any traffic itself: once a
// mapping is decided, the backend (docker or kubernetes) is what actually
// publishes it (see sandbox.EnvironmentConfig.PortMappings and
// EnvironmentBackend.RestartEnvironmentPorts's own doc comments) — no
// separate register/deregister step against anything agent-runner's own
// process holds open.
// -----------------------------------------------------------------------

// assignSSHPort assigns environmentID's ssh_port if it doesn't have one
// yet and the feature is configured on this deployment (both
// SSHPortRangeStart/End nonzero — see config.Settings.SSHBastionPortRangeStart's
// own doc comment), returning 0 otherwise. Only ever called once per
// environment in practice (from handleCreateEnvironment, before the
// backing container/Pod exists at all — the port must be known before
// CreateEnvironment can bake it in as a published binding), but written to
// be a safe no-op if called again against an environment that already has
// one.
func (s *Server) assignSSHPort(ctx context.Context, id uuid.UUID) int {
	if s.EnvironmentRepo == nil || s.SSHPortRangeStart == 0 || s.SSHPortRangeEnd < s.SSHPortRangeStart {
		return 0
	}
	env, err := s.EnvironmentRepo.FindEnvironmentByID(ctx, id)
	if err != nil {
		s.Log.Warn("acpbridge: assignSSHPort: failed to look up environment", "environment_id", id, "error", err)
		return 0
	}
	if env.SSHPort != nil {
		return *env.SSHPort
	}
	port, err := s.EnvironmentRepo.AssignSSHPort(ctx, id, s.SSHPortRangeStart, s.SSHPortRangeEnd)
	if err != nil {
		s.Log.Warn("acpbridge: failed to assign ssh port", "environment_id", id, "error", err)
		return 0
	}
	return port
}

// bootstrapSSHKeys renders backendRef's authorized_keys from every SSH key
// registered on environmentID and starts its sshd — called once, right
// after a fresh container/Pod exists with nothing pushed to it yet: from
// handleCreateEnvironment (a brand-new container) and, on the docker
// backend only, from handleRestartEnvironmentPorts (a recreated
// container — the kubernetes backend never touches the Pod on a restart,
// so its own authorized_keys are already intact and this is skipped
// there). Best-effort: a failure here is logged, never returned as the
// caller's own error — the container/Pod lifecycle operation itself
// already succeeded by the time this runs, and the handleSyncEnvironmentSSHKeys
// endpoint (unchanged by this file's port-mapping rework) can always
// re-push authorized_keys later.
func (s *Server) bootstrapSSHKeys(ctx context.Context, environmentID uuid.UUID, backendRef string) {
	if s.SSHKeyRepo == nil {
		return
	}
	keys, err := s.SSHKeyRepo.ListPublicKeys(ctx, environmentID)
	if err != nil {
		s.Log.Warn("acpbridge: failed to list ssh keys", "environment_id", environmentID, "error", err)
		keys = nil
	}
	if err := sandbox.BootstrapEnvironmentSSH(ctx, s.SandboxMgr, backendRef, keys); err != nil {
		s.Log.Warn("acpbridge: failed to bootstrap environment sshd", "environment_id", environmentID, "error", err)
	}
}

// buildPortMappings assembles the full desired container-port -> host-port
// set for environmentID: sshPort (0 if SSH isn't configured or not yet
// assigned) plus one entry per environment_port_forwards row, self-
// assigning a host port for any row that doesn't have one yet (mirrors
// assignSSHPort's own "safe to call repeatedly" idiom) when
// PortForwardRangeStart/End are configured. Used by handleCreateEnvironment
// (sshPort only — a brand-new environment has no forwards yet) and
// handleRestartEnvironmentPorts (the full set).
func (s *Server) buildPortMappings(ctx context.Context, environmentID uuid.UUID, sshPort int) []sandbox.PortMapping {
	var mappings []sandbox.PortMapping
	if sshPort != 0 {
		mappings = append(mappings, sandbox.PortMapping{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: sshPort})
	}
	if s.PortForwardRepo == nil {
		return mappings
	}
	forwards, err := s.PortForwardRepo.ListForEnvironment(ctx, environmentID)
	if err != nil {
		s.Log.Warn("acpbridge: buildPortMappings: failed to list port forwards", "environment_id", environmentID, "error", err)
		return mappings
	}
	for _, pf := range forwards {
		if pf.HostPort == nil {
			if s.PortForwardRangeStart == 0 || s.PortForwardRangeEnd < s.PortForwardRangeStart {
				// Feature not configured on this deployment — see
				// config.Settings.PortForwardRangeStart's own doc comment.
				// Never assign a port, never publish this row.
				continue
			}
			port, err := s.PortForwardRepo.AssignHostPort(ctx, pf.ID, s.PortForwardRangeStart, s.PortForwardRangeEnd)
			if err != nil {
				s.Log.Warn("acpbridge: failed to assign port forward host port", "environment_id", environmentID, "port_forward_id", pf.ID, "error", err)
				continue
			}
			pf.HostPort = &port
		}
		mappings = append(mappings, sandbox.PortMapping{ContainerPort: pf.ContainerPort, HostPort: *pf.HostPort})
	}
	return mappings
}

// -----------------------------------------------------------------------
// POST /internal/environments
// -----------------------------------------------------------------------

type createEnvironmentRequest struct {
	EnvironmentID string `json:"environment_id"`
	ProjectID     string `json:"project_id"`
	Image         string `json:"image"`
	CPULimit      string `json:"cpu_limit"`
	MemoryLimit   string `json:"memory_limit"`
	DiskLimitGB   int    `json:"disk_limit_gb"`
	SecretKey     string `json:"secret_key"`
}

type createEnvironmentResponse struct {
	Backend    string `json:"backend"`
	BackendRef string `json:"backend_ref"`
	VolumeRef  string `json:"volume_ref"`
	BaseURL    string `json:"base_url"`
	// SSHPort is 0 when the SSH feature isn't configured on this
	// deployment (config.Settings.SSHBastionPortRangeStart/End) — see
	// assignSSHPort's own doc comment. Port forwards have no equivalent
	// field here — they're user-managed, possibly many per environment,
	// so the frontend reads them back via their own list endpoint instead
	// of this lifecycle response.
	SSHPort int `json:"ssh_port"`
}

// handleCreateEnvironment provisions environment_id's backing
// container/Pod and volume via sandbox.EnvironmentBackend.CreateEnvironment
// and reports back what was created — backend, backend_ref, and volume_ref
// all come straight from the returned EnvironmentHandle, which the docker
// and kubernetes implementations populate with the real, authoritative
// values (not reconstructed here by naming-convention guesswork — see
// EnvironmentHandle's own doc comment for why that matters for a later
// DeleteEnvironment call).
//
// The SSH port, if configured, is assigned *before* CreateEnvironment is
// even called — a real ordering requirement, not a stylistic choice:
// unlike the old relay design (which registered a port against an
// already-running container after the fact), the port must already be
// known so CreateEnvironment can bake it in as a published binding (a
// Docker -p flag, a Kubernetes Service entry — see
// sandbox.EnvironmentConfig.PortMappings). A brand-new environment has no
// port-forward rows yet (nothing to add one against before it exists), so
// buildPortMappings only ever contributes the SSH entry here.
func (s *Server) handleCreateEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	var req createEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.EnvironmentID == "" {
		writeJSONError(w, http.StatusBadRequest, "environment_id is required")
		return
	}
	environmentID, err := uuid.Parse(req.EnvironmentID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid environment_id")
		return
	}

	sshPort := s.assignSSHPort(r.Context(), environmentID)

	handle, err := s.SandboxMgr.CreateEnvironment(r.Context(), sandbox.EnvironmentConfig{
		EnvironmentID: req.EnvironmentID,
		Image:         req.Image,
		CPULimit:      req.CPULimit,
		MemoryLimit:   req.MemoryLimit,
		DiskLimitGB:   req.DiskLimitGB,
		SecretKey:     req.SecretKey,
		PortMappings:  s.buildPortMappings(r.Context(), environmentID, sshPort),
	})
	if err != nil {
		s.Log.Error("acpbridge: failed to create environment", "environment_id", req.EnvironmentID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if sshPort != 0 {
		s.bootstrapSSHKeys(r.Context(), environmentID, handle.BackendRef)
	}

	writeJSON(w, http.StatusOK, createEnvironmentResponse{
		Backend:    handle.Backend,
		BackendRef: handle.BackendRef,
		VolumeRef:  handle.VolumeRef,
		BaseURL:    handle.BaseURL,
		SSHPort:    sshPort,
	})
}

// -----------------------------------------------------------------------
// POST /internal/environments/{id}/start
// -----------------------------------------------------------------------

type startEnvironmentRequest struct {
	BackendRef  string `json:"backend_ref"`
	Image       string `json:"image"`
	CPULimit    string `json:"cpu_limit"`
	MemoryLimit string `json:"memory_limit"`
	DiskLimitGB int    `json:"disk_limit_gb"`
	SecretKey   string `json:"secret_key"`
}

type startEnvironmentResponse struct {
	BaseURL string `json:"base_url"`
	// SSHPort — see createEnvironmentResponse's own doc comment.
	SSHPort int `json:"ssh_port"`
	// BackendRef is only ever different from the request's own when the
	// docker backend had to self-heal a container removed outside of Paca
	// (see docker.Manager.recreateGoneEnvironmentContainer) — a plain
	// restart of a still-existing container always echoes req.BackendRef
	// back unchanged. Empty in that ordinary case so services/api's own
	// "did this change" check (mirroring handleRestartEnvironmentPorts's
	// response) has nothing to act on.
	BackendRef string `json:"backend_ref,omitempty"`
}

// handleStartEnvironment restarts an existing container/Pod without
// touching its published port bindings at all — they're already fixed on
// the container (docker) or already live on its Service (kubernetes) from
// whichever CreateEnvironment/handleRestartEnvironmentPorts call last
// applied them. SSHPort is only read back from Postgres to echo in the
// response, matching createEnvironmentResponse's own shape.
//
// PortMappings is computed up front and passed through regardless: an
// ordinary restart of a still-existing container ignores it completely
// (see EnvironmentConfig.PortMappings's own doc comment), but the docker
// backend's self-heal path — recreating a container removed outside of
// Paca (see recreateGoneEnvironmentContainer's doc comment) — builds a
// brand-new container with no bindings of its own, so without this it
// would come back reachable over ACP but with none of the environment's
// real SSH/port-forward bindings republished.
//
// SSH is unconditionally re-bootstrapped here whenever sshPort != 0 — not
// only when StartEnvironment signals a self-heal recreate. sshd itself
// does not survive an ordinary stop: Docker's ContainerStop SIGKILLs
// everything in the container's PID namespace, and Kubernetes' scale-0→1
// always schedules a brand-new Pod, so a plain stop→start leaves sshd dead
// on both backends even when nothing else about the container/Pod
// changed. BootstrapEnvironmentSSH is idempotent/safe to call on an
// already-running environment (see its own doc comment) — the same
// unconditional treatment handleCreateEnvironment already gives it.
func (s *Server) handleStartEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	id := r.PathValue("id")
	environmentID, err := uuid.Parse(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	var req startEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BackendRef == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref is required")
		return
	}

	sshPort := 0
	if s.EnvironmentRepo != nil {
		if env, err := s.EnvironmentRepo.FindEnvironmentByID(r.Context(), environmentID); err == nil && env.SSHPort != nil {
			sshPort = *env.SSHPort
		}
	}
	portMappings := s.buildPortMappings(r.Context(), environmentID, sshPort)

	handle, err := s.SandboxMgr.StartEnvironment(r.Context(), req.BackendRef, sandbox.EnvironmentConfig{
		EnvironmentID: id,
		Image:         req.Image,
		CPULimit:      req.CPULimit,
		MemoryLimit:   req.MemoryLimit,
		DiskLimitGB:   req.DiskLimitGB,
		SecretKey:     req.SecretKey,
		PortMappings:  portMappings,
	})
	if err != nil {
		s.Log.Error("acpbridge: failed to start environment", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	recreatedBackendRef := ""
	if handle.BackendRef != "" && handle.BackendRef != req.BackendRef {
		recreatedBackendRef = handle.BackendRef
	}
	if sshPort != 0 {
		bootstrapRef := firstNonEmpty(recreatedBackendRef, req.BackendRef)
		s.bootstrapSSHKeys(r.Context(), environmentID, bootstrapRef)
	}

	writeJSON(w, http.StatusOK, startEnvironmentResponse{BaseURL: handle.BaseURL, SSHPort: sshPort, BackendRef: recreatedBackendRef})
}

// -----------------------------------------------------------------------
// POST /internal/environments/{id}/stop
// -----------------------------------------------------------------------

type stopEnvironmentRequest struct {
	BackendRef string `json:"backend_ref"`
}

// handleStopEnvironment stops the backing container/Pod — nothing else to
// do: stopping it already stops whatever was publishing its ports (a
// stopped Docker container's -p bindings accept no connections; a
// kubernetes Service's endpoint simply has no ready Pod behind it), so
// there is no separate deregister step the way the old relay design
// needed.
func (s *Server) handleStopEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	id := r.PathValue("id")
	var req stopEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BackendRef == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref is required")
		return
	}
	if err := s.SandboxMgr.StopEnvironment(r.Context(), req.BackendRef); err != nil {
		s.Log.Error("acpbridge: failed to stop environment", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{})
}

// -----------------------------------------------------------------------
// DELETE /internal/environments/{id}
// -----------------------------------------------------------------------

type deleteEnvironmentRequest struct {
	BackendRef string `json:"backend_ref"`
	VolumeRef  string `json:"volume_ref"`
}

func (s *Server) handleDeleteEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	id := r.PathValue("id")
	var req deleteEnvironmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BackendRef == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref is required")
		return
	}
	if err := s.SandboxMgr.DeleteEnvironment(r.Context(), req.BackendRef, req.VolumeRef); err != nil {
		s.Log.Error("acpbridge: failed to delete environment", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{})
}

// -----------------------------------------------------------------------
// Folder provisioning — POST /internal/environments/{id}/folders
//
// A "folder" is just a pointer to a working directory inside the
// environment's filesystem an agent should use — services/api's own
// `environment_folders` row owns that pointer. Deleting a folder therefore
// only ever means "stop tracking this directory" (a services/api-only DB
// delete, no call into this package at all — see
// environmentsvc.Service.DeleteFolder) — never "destroy its contents", so
// there is deliberately no DELETE endpoint here.
//
// mkdir -p on create still needs a top-level-system-directory guard (an
// agent-supplied path landing on /etc or /root would be surprising even
// though mkdir itself isn't destructive) — ports
// apps/mcp/src/tools/repo-tools.ts's FORBIDDEN_DELETE_TARGETS guard to Go
// for that purpose. environmentHomeRoot below is the environment
// filesystem root (see docs/ai-agent/environment-management.md's Docker
// section — deliberately not /home/goose, so the ephemeral conversation
// path and the environment path never collide).
// -----------------------------------------------------------------------

// environmentHomeRoot is the fixed mount root every static environment's
// folders live under — see internal/sandbox/docker/environment.go /
// internal/sandbox/k8s/environment.go (owned by a parallel change; not
// read here, just relied on as a documented constant both are expected to
// honor).
const environmentHomeRoot = "/home/paca/workspaces"

// forbiddenFolderTargets is FORBIDDEN_DELETE_TARGETS
// (apps/mcp/src/tools/repo-tools.ts) ported for the environment
// filesystem: the goose sandbox image's own top-level directories, plus
// /home/paca and /home/paca/workspaces (this environment's actual home and
// its folders root, the direct analogs of that file's own /home/goose
// entry) — never a valid mkdir target, however a caller's path got there.
var forbiddenFolderTargets = map[string]bool{
	"/":                 true,
	"/bin":              true,
	"/boot":             true,
	"/dev":              true,
	"/etc":              true,
	"/home":             true,
	"/home/paca":        true,
	environmentHomeRoot: true,
	"/lib":              true,
	"/lib64":            true,
	"/opt":              true,
	"/proc":             true,
	"/root":             true,
	"/run":              true,
	"/sbin":             true,
	"/srv":              true,
	"/sys":              true,
	"/tmp":              true,
	"/usr":              true,
	"/var":              true,
}

// safeFolderTarget mirrors repo-tools.ts's assertSafeDeleteTarget: resolves
// targetDir (normalizing "../"/"./" segments against root) and refuses it
// if that resolved path is one of forbiddenFolderTargets. Returns the
// resolved path so the caller execs *that* — not the raw, unnormalized
// targetDir — closing the traversal a caller could otherwise use to slip
// e.g. "../etc" past this check (which normalizes to "/etc", correctly
// refused) while a raw exec would still act on the literal ".." segments.
func safeFolderTarget(targetDir string) (string, error) {
	resolved := path.Clean("/" + targetDir)
	if forbiddenFolderTargets[resolved] {
		return "", fmt.Errorf("refusing to operate on %s — it's a protected system directory, not a valid folder path", resolved)
	}
	return resolved, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

type createEnvironmentFolderRequest struct {
	BackendRef string `json:"backend_ref"`
	Path       string `json:"path"`
}

// handleCreateEnvironmentFolder runs `mkdir -p <path>` inside backend_ref —
// mirrors docs/ai-agent/environment-management.md's Folders section.
// Folders are path-only (no repo-clone step) — cloning a repo into a
// folder, if wanted, is left to the agent's own tool use or a manual `git
// clone` over SSH/terminal once the folder exists.
func (s *Server) handleCreateEnvironmentFolder(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	id := r.PathValue("id")
	var req createEnvironmentFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BackendRef == "" || req.Path == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref and path are required")
		return
	}
	resolved, err := safeFolderTarget(req.Path)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	out, exitCode, err := s.SandboxMgr.ExecEnvironment(r.Context(), req.BackendRef, []string{"mkdir", "-p", resolved})
	if err != nil || exitCode != 0 {
		s.Log.Error("acpbridge: failed to mkdir environment folder",
			"environment_id", id, "path", resolved, "output", out, "error", err)
		writeJSONError(w, http.StatusInternalServerError,
			fmt.Sprintf("mkdir -p %s failed: %s", resolved, firstNonEmpty(errString(err), out)))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{})
}

// -----------------------------------------------------------------------
// GET /internal/environments/{id}/browse
// -----------------------------------------------------------------------

type browseEnvironmentEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
}

// handleBrowseEnvironment lists the immediate children of ?path= (defaults
// to environmentHomeRoot) inside ?backend_ref='s running container/Pod —
// the folder-creation UI's "browse instead of typing blind" affordance
// (services/api's FolderService.Browse). Read-only, and deliberately
// scoped to environmentHomeRoot or below (the same root safeFolderTarget
// protects against mkdir'ing outside of) rather than the whole container
// filesystem, since this is reachable from any project member with read
// access, not just write.
//
// Listing goes through `find`, not `ls`, so the type/name pairs can be
// NUL-delimited (find's -printf supports \0 as a literal escape) — robust
// against filenames containing spaces or even newlines, with no shell
// quoting to get wrong since ExecEnvironment passes cmd as argv directly,
// never through a shell. A non-zero exit (most commonly: path doesn't
// exist yet, e.g. the user is browsing toward a folder they're about to
// create) is treated as an empty listing rather than an error — browsing
// is inherently a "let me peek" affordance, and a missing directory isn't
// a hard failure the way a failed mkdir/rm-rf would be.
func (s *Server) handleBrowseEnvironment(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	backendRef := r.URL.Query().Get("backend_ref")
	if backendRef == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref is required")
		return
	}
	reqPath := r.URL.Query().Get("path")
	if reqPath == "" {
		reqPath = environmentHomeRoot
	}
	resolved := path.Clean("/" + reqPath)
	if resolved != environmentHomeRoot && !strings.HasPrefix(resolved, environmentHomeRoot+"/") {
		writeJSONError(w, http.StatusBadRequest,
			fmt.Sprintf("can only browse within %s", environmentHomeRoot))
		return
	}

	out, exitCode, err := s.SandboxMgr.ExecEnvironment(r.Context(), backendRef,
		[]string{"find", resolved, "-mindepth", "1", "-maxdepth", "1", "-printf", "%y\x00%f\x00"})
	entries := []browseEnvironmentEntry{}
	if err == nil && exitCode == 0 {
		parts := strings.Split(out, "\x00")
		for i := 0; i+1 < len(parts); i += 2 {
			if parts[i] == "" && parts[i+1] == "" {
				continue
			}
			entries = append(entries, browseEnvironmentEntry{
				Name:  parts[i+1],
				IsDir: parts[i] == "d",
			})
		}
		sort.Slice(entries, func(a, b int) bool {
			if entries[a].IsDir != entries[b].IsDir {
				return entries[a].IsDir
			}
			return strings.ToLower(entries[a].Name) < strings.ToLower(entries[b].Name)
		})
	}
	// A non-zero exit/exec error is intentionally swallowed into an empty
	// listing rather than surfaced — see doc comment above.

	writeJSON(w, http.StatusOK, map[string]any{
		"path":    resolved,
		"entries": entries,
	})
}

// -----------------------------------------------------------------------
// POST /internal/environments/{id}/ssh-keys/sync
// -----------------------------------------------------------------------

type syncEnvironmentSSHKeysRequest struct {
	BackendRef string `json:"backend_ref"`
}

// handleSyncEnvironmentSSHKeys re-renders and re-pushes a running
// environment's authorized_keys from environment_ssh_keys — called by
// services/api's AddSSHKey/DeleteSSHKey (environmentsvc.Service) right
// after each writes to that table, so a key registered/revoked on an
// already-running environment takes effect immediately rather than
// waiting for its next Start. Deliberately does not restart sshd — it
// re-reads AuthorizedKeysFile on every connection attempt, see
// sandbox.SyncEnvironmentAuthorizedKeys's own doc comment — and does
// nothing to a stopped/never-created environment (there's no container to
// push a file into yet; its next Start renders the current keys anyway
// via syncSSHRoute).
func (s *Server) handleSyncEnvironmentSSHKeys(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	if s.SSHKeyRepo == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	id := r.PathValue("id")
	var req syncEnvironmentSSHKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BackendRef == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref is required")
		return
	}

	environmentID, err := uuid.Parse(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	keys, err := s.SSHKeyRepo.ListPublicKeys(r.Context(), environmentID)
	if err != nil {
		s.Log.Error("acpbridge: failed to list ssh keys for sync", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := sandbox.SyncEnvironmentAuthorizedKeys(r.Context(), s.SandboxMgr, req.BackendRef, keys); err != nil {
		s.Log.Error("acpbridge: failed to sync environment ssh keys", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// -----------------------------------------------------------------------
// POST /internal/environments/{id}/port-forwards/assign
// -----------------------------------------------------------------------

// handlePortForwardsAssign assigns a host port to every not-yet-assigned
// port-forward row belonging to id — called by services/api's
// AddPortForward right after it inserts a new row, so the frontend can
// show the assigned port immediately. This decides *which number* a
// forward will eventually publish on; it does not touch the backing
// container/Pod/Service at all — that only happens the next time the
// environment is (re)started, via handleRestartEnvironmentPorts below (see
// this feature's "restart required" UX — docs/ai-agent/
// environment-management.md's "Port Forwarding" section).
func (s *Server) handlePortForwardsAssign(w http.ResponseWriter, r *http.Request) {
	if s.PortForwardRepo == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	id := r.PathValue("id")
	environmentID, err := uuid.Parse(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	if s.PortForwardRangeStart == 0 || s.PortForwardRangeEnd < s.PortForwardRangeStart {
		// Feature not configured on this deployment — see
		// config.Settings.PortForwardRangeStart's own doc comment. Never
		// assign a port.
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	forwards, err := s.PortForwardRepo.ListForEnvironment(r.Context(), environmentID)
	if err != nil {
		s.Log.Error("acpbridge: failed to list port forwards to assign", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, pf := range forwards {
		if pf.HostPort != nil {
			continue
		}
		if _, err := s.PortForwardRepo.AssignHostPort(r.Context(), pf.ID, s.PortForwardRangeStart, s.PortForwardRangeEnd); err != nil {
			s.Log.Warn("acpbridge: failed to assign port forward host port", "environment_id", id, "port_forward_id", pf.ID, "error", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// -----------------------------------------------------------------------
// POST /internal/environments/{id}/restart-ports
// -----------------------------------------------------------------------

type restartEnvironmentPortsRequest struct {
	BackendRef  string `json:"backend_ref"`
	VolumeRef   string `json:"volume_ref"`
	Image       string `json:"image"`
	CPULimit    string `json:"cpu_limit"`
	MemoryLimit string `json:"memory_limit"`
	DiskLimitGB int    `json:"disk_limit_gb"`
	SecretKey   string `json:"secret_key"`
}

type restartEnvironmentPortsResponse struct {
	BackendRef string `json:"backend_ref"`
	BaseURL    string `json:"base_url"`
	SSHPort    int    `json:"ssh_port"`
}

// handleRestartEnvironmentPorts applies the environment's full current
// port-mapping set (its SSH port plus every environment_port_forwards
// row) to its backing container/Pod — called by services/api's
// StartEnvironment (when the environment's ports_pending_restart flag is
// set) and its explicit RestartEnvironment action (a user clicking
// "Restart" on a running environment to apply a just-added/removed
// forward). See EnvironmentBackend.RestartEnvironmentPorts's own doc
// comment for why a plain /start can't do this: on docker, req.BackendRef
// may not match the container ID this returns (bindings are fixed at
// create time, so applying a new set means recreating it); on kubernetes,
// BackendRef is unchanged (only a Service gets patched, the Pod is never
// touched).
func (s *Server) handleRestartEnvironmentPorts(w http.ResponseWriter, r *http.Request) {
	if !s.requireSandboxMgr(w) {
		return
	}
	id := r.PathValue("id")
	environmentID, err := uuid.Parse(id)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid environment id")
		return
	}
	var req restartEnvironmentPortsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BackendRef == "" || req.VolumeRef == "" {
		writeJSONError(w, http.StatusBadRequest, "backend_ref and volume_ref are required")
		return
	}

	sshPort := s.assignSSHPort(r.Context(), environmentID)

	handle, err := s.SandboxMgr.RestartEnvironmentPorts(r.Context(), req.BackendRef, req.VolumeRef, sandbox.EnvironmentConfig{
		EnvironmentID: id,
		Image:         req.Image,
		CPULimit:      req.CPULimit,
		MemoryLimit:   req.MemoryLimit,
		DiskLimitGB:   req.DiskLimitGB,
		SecretKey:     req.SecretKey,
		PortMappings:  s.buildPortMappings(r.Context(), environmentID, sshPort),
	})
	if err != nil {
		s.Log.Error("acpbridge: failed to restart environment ports", "environment_id", id, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Only the docker backend's container actually changed identity and
	// lost its authorized_keys — the kubernetes backend never touched the
	// Pod (see RestartEnvironmentPorts's own doc comment on both
	// backends), so re-bootstrapping there would be redundant work against
	// a Pod that already has everything it needs.
	if sshPort != 0 && s.Backend != "kubernetes" {
		s.bootstrapSSHKeys(r.Context(), environmentID, handle.BackendRef)
	}

	writeJSON(w, http.StatusOK, restartEnvironmentPortsResponse{
		BackendRef: handle.BackendRef,
		BaseURL:    handle.BaseURL,
		SSHPort:    sshPort,
	})
}
