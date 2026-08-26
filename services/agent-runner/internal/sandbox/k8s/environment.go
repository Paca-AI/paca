// environment.go implements sandbox.EnvironmentBackend on *Manager — the
// long-lived counterpart to Start/Stop/CopyToContainer/Exec in manager.go.
// A batch/v1.Job (manager.go's own primitive, BackoffLimit 0,
// run-to-completion) is the wrong tool here: Jobs don't restart, and a
// static environment is meant to survive an unbounded number of
// stop/start cycles across its life. This file uses an apps/v1.Deployment
// (always exactly 1 replica — StartEnvironment/StopEnvironment just
// toggle spec.replicas between 1 and 0) plus a separately-created
// PersistentVolumeClaim, rather than a StatefulSet: environments are
// always exactly one replica with no peer-discovery need, so a plain
// Deployment plus one hand-created PVC is simpler than a StatefulSet's
// per-replica PVC templating machinery, which exists to solve a problem
// (N replicas, N PVCs, stable pod identity per ordinal) this package
// doesn't have.
package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

const (
	// environmentLabel is set (to the owning Deployment's own name — see
	// environmentName) on every Pod an environment's Deployment creates.
	// It's what every method below that only receives backendRef, not the
	// raw EnvironmentID (Stop/Delete/Copy/Exec/StreamExec — see
	// sandbox.EnvironmentBackend's interface, which deliberately doesn't
	// thread EnvironmentID through those calls), selects Pods by.
	// Kubernetes has no built-in equivalent for a Deployment the way it
	// auto-sets job-name on every Job's Pods (see manager.go's
	// labelConvID/labelManaged and waitForPodIP's selector), so this
	// package sets one explicitly.
	environmentLabel = "paca.ai/environment"

	// environmentWorkspaceRoot is where an environment's
	// PersistentVolumeClaim is mounted inside its Deployment's container —
	// deliberately not /home/goose, the ephemeral per-conversation
	// sandbox's own home directory (see services/agent-server/Dockerfile),
	// so an agent that's ever attached to both kinds of sandbox never sees
	// the two paths collide.
	environmentWorkspaceRoot = "/home/paca/workspaces"

	// defaultDiskLimitGB backs EnvironmentConfig.DiskLimitGB when unset
	// (<= 0) — a PersistentVolumeClaim needs a nonzero storage request,
	// and unlike cpuLimit/memoryLimit (whose zero-value fallback is a
	// Manager-wide Options default), disk sizing is inherently
	// per-environment, so there's no equivalent Options field to fall
	// back to. Sized for a typical repo checkout plus language-toolchain
	// caches without over-provisioning by default.
	defaultDiskLimitGB = 10
)

// environmentName derives the shared name used for both an environment's
// Deployment and its PersistentVolumeClaim from cfg.EnvironmentID —
// mirrors jobName's own role for the Job backend (see manager.go): one
// deterministic name lets StartEnvironment/StopEnvironment/
// DeleteEnvironment/CopyToEnvironment/ExecEnvironment/
// StreamExecEnvironment address the right objects from backendRef alone,
// with nothing cached on EnvironmentHandle beyond that name. Reusing the
// same name for both the Deployment and the PVC is safe — Kubernetes
// scopes object identity to (namespace, kind, name), not name alone, so a
// Deployment and a PVC sharing a name never collide.
func environmentName(environmentID string) string {
	return "paca-env-" + environmentID
}

// resolveEnvironmentImage returns cfgImage if set, otherwise fallback (in
// practice, m.agentServerImage) — pulled out of CreateEnvironment as its
// own pure function so the "empty means platform default" contract
// documented on sandbox.EnvironmentConfig.Image is unit-testable without
// standing up a real Deployment/Pod.
func resolveEnvironmentImage(cfgImage, fallback string) string {
	if cfgImage != "" {
		return cfgImage
	}
	return fallback
}

// resolveEnvironmentDiskLimitGB returns cfgLimit if positive, otherwise
// defaultDiskLimitGB — pulled out of CreateEnvironment for the same
// direct-unit-testability reason as resolveEnvironmentImage.
func resolveEnvironmentDiskLimitGB(cfgLimit int) int {
	if cfgLimit <= 0 {
		return defaultDiskLimitGB
	}
	return cfgLimit
}

// environmentServicePortName names one ServicePort entry — "ssh" for the
// environment's own fixed SSH port, "pf-<container_port>" for a user
// port-forward, collision-free since environment_port_forwards enforces
// container_port uniqueness per environment (see migration
// 000042_add_environments.sql's uq_environment_port_forwards_container_port).
func environmentServicePortName(containerPort int) string {
	if containerPort == sandbox.EnvironmentSSHPort {
		return "ssh"
	}
	return fmt.Sprintf("pf-%d", containerPort)
}

// environmentServicePorts converts mappings to the ServicePort list a
// NodePort Service publishes them as — Port/TargetPort both set to the
// container port (this Service is only ever reached via its NodePort from
// outside the cluster, never via its ClusterIP, so Port's own value is
// otherwise unused) and NodePort pinned to our own already-assigned host
// port rather than left for Kubernetes to pick, mirroring exactly which
// port a Docker -p binding would have used.
func environmentServicePorts(mappings []sandbox.PortMapping) []corev1.ServicePort {
	ports := make([]corev1.ServicePort, 0, len(mappings))
	for _, m := range mappings {
		ports = append(ports, corev1.ServicePort{
			Name:       environmentServicePortName(m.ContainerPort),
			Port:       int32(m.ContainerPort),
			TargetPort: intOrStringFromInt(m.ContainerPort),
			NodePort:   int32(m.HostPort),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	return ports
}

// ensureEnvironmentService brings name's NodePort Service in line with
// mappings — created if missing and mappings is non-empty, patched in
// place (a plain "replace spec.ports wholesale" merge patch, not a
// per-port diff) if it already exists, or deleted if mappings is now empty
// (a Service's spec.ports must be non-empty, and there is nothing left to
// publish once a user's only forward is removed and SSH isn't configured
// either). Called by CreateEnvironment (mappings containing at most the
// SSH entry — nothing else can exist yet for a brand-new environment) and
// RestartEnvironmentPorts (the full current set) alike; unlike the
// Deployment/Pod, this never requires recreating anything — a Service's
// port list is freely mutable in place, which is exactly why adding or
// removing a port forward needs no Pod disruption on this backend, unlike
// docker's RestartEnvironmentPorts.
func (m *Manager) ensureEnvironmentService(ctx context.Context, name string, labels map[string]string, mappings []sandbox.PortMapping) error {
	svcClient := m.clientset.CoreV1().Services(m.namespace)

	if len(mappings) == 0 {
		if err := svcClient.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("sandbox/k8s: delete environment service %s: %w", name, err)
		}
		return nil
	}

	ports := environmentServicePorts(mappings)
	_, err := svcClient.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.namespace, Labels: labels},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: labels,
				Ports:    ports,
			},
		}
		if _, err := svcClient.Create(ctx, svc, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("sandbox/k8s: create environment service %s: %w", name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("sandbox/k8s: get environment service %s: %w", name, err)
	}

	portsJSON, err := json.Marshal(ports)
	if err != nil {
		return fmt.Errorf("sandbox/k8s: marshal environment service ports: %w", err)
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"ports":%s}}`, portsJSON))
	if _, err := svcClient.Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("sandbox/k8s: patch environment service %s: %w", name, err)
	}
	return nil
}

// intOrStringFromInt is a tiny local alias so environmentServicePorts reads
// cleanly without importing apimachinery's intstr package under its own
// name just for this one call site.
func intOrStringFromInt(port int) intstr.IntOrString {
	return intstr.FromInt32(int32(port))
}

// environmentResources computes Requests==Limits for an environment's
// container, mirroring Start's own resources block in manager.go but with
// a per-environment override: unlike sandbox.Config (Start's cfg, which
// has no CPU/MemoryLimit fields at all — every ephemeral sandbox always
// uses this Manager's process-wide cpuLimit/memoryLimit),
// sandbox.EnvironmentConfig carries its own CPULimit/MemoryLimit, letting
// one environment be sized differently from another; empty falls back to
// the same Manager-wide default Start itself uses.
func (m *Manager) environmentResources(cfg sandbox.EnvironmentConfig) (corev1.ResourceRequirements, error) {
	cpu := m.cpuLimit
	if cfg.CPULimit != "" {
		q, err := resource.ParseQuantity(cfg.CPULimit)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("sandbox/k8s: parse environment CPU limit %q: %w", cfg.CPULimit, err)
		}
		cpu = q
	}
	mem := m.memoryLimit
	if cfg.MemoryLimit != "" {
		q, err := resource.ParseQuantity(cfg.MemoryLimit)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("sandbox/k8s: parse environment memory limit %q: %w", cfg.MemoryLimit, err)
		}
		mem = q
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: cpu, corev1.ResourceMemory: mem},
	}, nil
}

// CreateEnvironment provisions a new static environment's backing
// PersistentVolumeClaim and Deployment (1 replica) and waits for it to
// become reachable — see the package doc comment on why a Deployment, not
// the batch/v1.Job Start uses, is the right primitive here.
//
// Unlike Start, which lets its caller (executor.go) resolve cfg.Image
// down to config.AgentServerImage before this package ever sees it,
// CreateEnvironment resolves that default itself when cfg.Image is empty
// — see sandbox.EnvironmentConfig.Image's own doc comment for why: an
// environment's image must stay resolvable to the same default on every
// subsequent StartEnvironment too, not just at creation, so the fallback
// lives here rather than in a caller that only ever runs once.
//
// On a failure after the Deployment is created, the Deployment (but not
// the PVC) is cleaned up — mirroring Start's own cleanup-on-failure
// stance, but deliberately not extended to the PVC: unlike a Job's Pod,
// which never held anything worth keeping, a PVC that made it far enough
// to be provisioned may already hold real disk state (or at least be
// mid-provisioning by a CSI driver), and DeleteEnvironment is meant to be
// the only irreversible teardown in this file — see that method's own doc
// comment.
func (m *Manager) CreateEnvironment(ctx context.Context, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	if cfg.MCPDevSourceDir != "" {
		return nil, errors.New("sandbox/k8s: MCPDevSourceDir is not supported by the kubernetes backend — " +
			"see Manager.Start's identical guard in manager.go; use the docker backend for local apps/mcp development instead")
	}

	name := environmentName(cfg.EnvironmentID)
	image := resolveEnvironmentImage(cfg.Image, m.agentServerImage)
	diskLimitGB := resolveEnvironmentDiskLimitGB(cfg.DiskLimitGB)
	storageQty, err := resource.ParseQuantity(fmt.Sprintf("%dGi", diskLimitGB))
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: parse environment disk limit %dGi: %w", diskLimitGB, err)
	}

	labels := map[string]string{
		environmentLabel: name,
		labelManaged:     managedValue,
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.namespace, Labels: labels},
		Spec: corev1.PersistentVolumeClaimSpec{
			// ReadWriteOnce, not ReadWriteMany: an environment is always
			// exactly one Pod (unlike the plugins PVC elsewhere in this
			// codebase, which needs RWX for concurrent access from
			// multiple sandboxes at once), so there's never a second Pod
			// needing concurrent access to this same volume.
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: storageQty},
			},
		},
	}
	if m.environmentsStorageClassName != "" {
		pvc.Spec.StorageClassName = ptr.To(m.environmentsStorageClassName)
	}
	if _, err := m.clientset.CoreV1().PersistentVolumeClaims(m.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("sandbox/k8s: create environment pvc %s: %w", name, err)
	}

	resources, err := m.environmentResources(cfg)
	if err != nil {
		return nil, err
	}

	// cfg.Env first, so the hardcoded secret-key var below always wins on
	// a name collision — same ordering rationale as Start's own env
	// construction in manager.go. Unlike Start, there's no GitCommitter*
	// pair here: EnvironmentConfig carries no per-conversation git
	// identity — that's resolved per-conversation at ExecEnvironment/
	// ACP-session time instead, not baked into the long-lived container.
	env := make([]corev1.EnvVar, 0, len(cfg.Env)+2)
	for k, v := range cfg.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}
	env = append(env, corev1.EnvVar{Name: "GOOSE_SERVER__SECRET_KEY", Value: cfg.SecretKey})
	if cfg.DockerEnabled {
		// Sidecar and primary container share one Pod's network namespace
		// — see dind.go's package doc comment — so "localhost" already
		// reaches it directly, same as manager.go's own ephemeral Start.
		env = append(env, corev1.EnvVar{Name: "DOCKER_HOST", Value: "tcp://localhost:2375"})
	}

	containers := []corev1.Container{{
		Name:         containerName,
		Image:        image,
		Args:         []string{"serve", "--host", "0.0.0.0", "--port", strconv.Itoa(sandbox.GooseServePort)},
		Env:          env,
		Resources:    resources,
		Ports:        []corev1.ContainerPort{{ContainerPort: int32(sandbox.GooseServePort)}},
		VolumeMounts: []corev1.VolumeMount{{Name: "workspace", MountPath: environmentWorkspaceRoot}},
		// See environmentSecurityContext's own doc comment in
		// manager.go — Start's Job container hardening plus
		// the two extra capabilities real sshd needs, root
		// still the default user.
		SecurityContext: environmentSecurityContext(),
	}}
	if cfg.DockerEnabled {
		containers = append(containers, dindContainer(resources))
	}

	terminationGrace := int64(sandbox.StopTimeout.Seconds())
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken:  ptr.To(false),
					TerminationGracePeriodSeconds: &terminationGrace,
					ImagePullSecrets:              m.imagePullSecrets,
					Containers:                    containers,
					Volumes: []corev1.Volume{{
						Name: "workspace",
						VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: name},
						},
					}},
				},
			},
		},
	}
	if _, err := m.clientset.AppsV1().Deployments(m.namespace).Create(ctx, deployment, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("sandbox/k8s: create environment deployment %s: %w", name, err)
	}

	cleanup := func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.deleteDeployment(removeCtx, name)
		_ = m.ensureEnvironmentService(removeCtx, name, labels, nil)
	}

	// A brand-new environment has no port-forward rows yet — cfg.PortMappings
	// carries at most the environment's own SSH entry, assigned by the
	// caller (internal/acpbridge/environment_handlers.go's
	// handleCreateEnvironment) before this method was ever called. See
	// ensureEnvironmentService's own doc comment for why nothing is created
	// at all when this is empty (SSH not configured on this deployment).
	if err := m.ensureEnvironmentService(ctx, name, labels, cfg.PortMappings); err != nil {
		cleanup()
		return nil, err
	}

	selector := environmentLabel + "=" + name
	podName, podIP, err := m.waitForPodIP(ctx, selector)
	if err != nil {
		diag := m.diagnoseUnreadyEnvironment(context.Background(), name)
		cleanup()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	baseURL, err := sandbox.WaitForReady(ctx, []string{fmt.Sprintf("http://%s:%d", podIP, sandbox.GooseServePort)}, cfg.SecretKey)
	if err != nil {
		diag := m.diagnoseUnreadyEnvironment(context.Background(), name)
		cleanup()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	if cfg.DockerEnabled {
		if err := m.waitForDindReady(ctx, podName); err != nil {
			diag := m.diagnoseUnreadyEnvironment(context.Background(), name)
			cleanup()
			return nil, fmt.Errorf("sandbox/k8s: environment dind sidecar not ready: %w (%s)", err, diag)
		}
	}

	return &sandbox.EnvironmentHandle{BackendRef: name, BaseURL: baseURL, Backend: "kubernetes", VolumeRef: name}, nil
}

// RestartEnvironmentPorts applies cfg.PortMappings to backendRef's
// NodePort Service — see EnvironmentBackend.RestartEnvironmentPorts's own
// doc comment for the cross-backend contract, and ensureEnvironmentService's
// for why this never touches the Deployment/Pod at all on Kubernetes: a
// Service's port list is freely mutable in place, unlike a Docker
// container's fixed-at-create-time bindings. volumeRef is accepted to
// satisfy EnvironmentBackend's signature but unused — nothing about the
// PersistentVolumeClaim ever changes here.
func (m *Manager) RestartEnvironmentPorts(ctx context.Context, backendRef, volumeRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	labels := map[string]string{environmentLabel: backendRef, labelManaged: managedValue}
	if err := m.ensureEnvironmentService(ctx, backendRef, labels, cfg.PortMappings); err != nil {
		return nil, err
	}

	selector := environmentLabel + "=" + backendRef
	_, podIP, err := m.waitForPodIP(ctx, selector)
	if err != nil {
		diag := m.diagnoseUnreadyEnvironment(context.Background(), backendRef)
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}
	baseURL, err := sandbox.WaitForReady(ctx, []string{fmt.Sprintf("http://%s:%d", podIP, sandbox.GooseServePort)}, cfg.SecretKey)
	if err != nil {
		diag := m.diagnoseUnreadyEnvironment(context.Background(), backendRef)
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	return &sandbox.EnvironmentHandle{BackendRef: backendRef, BaseURL: baseURL}, nil
}

// ensureEnvironmentEnv backfills cfg.Env onto backendRef's Deployment the
// one time it's actually needed: a Deployment created by
// handleCreateEnvironment (which has no agent/LLM context at all — an
// environment isn't owned by one agent) never got
// GOOSE_PROVIDER/GOOSE_MODEL/the provider's API key baked in, so goose
// serve fails every session/new with "Configuration value not found:
// GOOSE_PROVIDER" until this runs once.
//
// Deliberately narrow, per StartEnvironment's own historical doc note on
// this exact gap: this only APPENDS cfg.Env keys the existing container
// spec's Env slice doesn't already have, preserving every existing entry
// (and its order) untouched — never reconstructs the whole list from
// cfg.Env's own (non-deterministic) map iteration order. That's what
// keeps this a true one-time patch: GOOSE_PROVIDER's presence is the
// check, so once applied, every later call short-circuits before ever
// touching the Deployment again, instead of risking a spurious rolling
// restart on every single conversation attach. A real patch here does
// roll the Pod once — Kubernetes has no live "update a running
// container's env" primitive either, same tradeoff Docker's
// recreateEnvironmentIfMissingEnv takes.
func (m *Manager) ensureEnvironmentEnv(ctx context.Context, backendRef string, cfg sandbox.EnvironmentConfig) error {
	if cfg.Env["GOOSE_PROVIDER"] == "" {
		return nil
	}
	depClient := m.clientset.AppsV1().Deployments(m.namespace)
	dep, err := depClient.Get(ctx, backendRef, metav1.GetOptions{})
	if err != nil {
		// Let the caller's own scaleDeployment (which already handles
		// apierrors.IsNotFound specially) surface this.
		return nil
	}
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return nil
	}
	container := &dep.Spec.Template.Spec.Containers[0]
	existing := make(map[string]bool, len(container.Env))
	for _, ev := range container.Env {
		existing[ev.Name] = true
	}
	if existing["GOOSE_PROVIDER"] {
		return nil
	}
	for k, v := range cfg.Env {
		if existing[k] {
			continue
		}
		container.Env = append(container.Env, corev1.EnvVar{Name: k, Value: v})
	}
	if _, err := depClient.Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("sandbox/k8s: update environment deployment %s to backfill env: %w", backendRef, err)
	}
	return nil
}

// pacaInfraEnvKeys — see docker/environment.go's own copy of this exact
// list and its doc comment for why these specifically need to stay in sync
// with this process's own current config on every StartEnvironment call,
// unlike GOOSE_PROVIDER/GOOSE_MODEL/the LLM API key (frozen forever to
// whichever agent's conversation first attached this environment).
// PACA_API_KEY/PACA_API_URL/PACA_GATEWAY_URL are deployment-level;
// PACA_WORKDIR/PACA_ACTOR_USER_ID/PACA_REPO_PLUGIN_IDS are conversation-
// scoped (track trigger.Workdir/ActorUserID/RepoPluginIDs) but need the
// same treatment for the same reason: Goose's EnvKeys lookup reads each
// from the Pod's own env, which is fixed at Pod-creation time.
// PACA_AGENT_ID/PACA_PROJECT_ID are deliberately excluded — see docker/
// environment.go's copy of this list for why resyncing those specifically
// would risk silently reassigning a live environment's frozen identity.
var pacaInfraEnvKeys = []string{
	"PACA_API_KEY", "PACA_API_URL", "PACA_GATEWAY_URL",
	"PACA_WORKDIR", "PACA_ACTOR_USER_ID", "PACA_REPO_PLUGIN_IDS",
	// GOOSE_PATH_ROOT — see docker/environment.go's copy of this list for
	// why it's included here rather than getting its own GOOSE_PROVIDER-
	// style one-time backfill.
	"GOOSE_PATH_ROOT",
}

// ensureEnvironmentInfraEnv keeps pacaInfraEnvKeys in sync with cfg.Env on
// every StartEnvironment call — the Kubernetes counterpart to
// docker/environment.go's identically-named function; see that function's
// doc comment for the full "why" (a Deployment baked before the platform's
// PACA_API_KEY was configured, before a rotation, or before a conversation
// targeting a different environment_folder set a new PACA_WORKDIR,
// otherwise carries a stale/missing key forever — a missing PACA_WORKDIR
// in particular makes Goose refuse to load the "paca" extension at all
// with "Configuration value not found: PACA_WORKDIR", not just silently
// zero out its tools the way a stale PACA_API_KEY does).
//
// Only meaningfully differs from ensureEnvironmentEnv's own no-op case
// right after that function has just backfilled: at that point cfg.Env's
// infra keys are already freshly applied, so this simply finds nothing
// stale and returns — no extra guard needed to skip calling it.
//
// Unlike ensureEnvironmentEnv's append-only "never touch an existing key"
// rule (a genuinely one-time patch), this REPLACES an existing
// pacaInfraEnvKeys entry when its value has changed, and removes one whose
// current cfg.Env value is now empty — every other env var on the
// container (GOOSE_PROVIDER, the agent's own PACA_AGENT_ID/
// PACA_PROJECT_ID, any user-configured env vars) is left untouched, so
// this can never silently reassign this environment's frozen LLM/agent
// identity to whichever conversation happens to trigger the sync.
func (m *Manager) ensureEnvironmentInfraEnv(ctx context.Context, backendRef string, cfg sandbox.EnvironmentConfig) error {
	if len(cfg.Env) == 0 {
		// A caller with no infra-env context at all — e.g.
		// handleStartEnvironment's plain restart, whose EnvironmentConfig
		// never sets Env (see its own doc comment: "restarts ... without
		// touching" what it doesn't own) — has nothing to reconcile
		// pacaInfraEnvKeys against. Every real attach path (coldStartEnvironment)
		// always populates cfg.Env with at least GOOSE_PROVIDER/GOOSE_MODEL/the
		// API key, so this only ever fires for that kind of context-free
		// caller; treating a nil/empty cfg.Env as "clear every key" instead
		// would strip PACA_API_KEY, GOOSE_PATH_ROOT, etc. from an
		// already-correctly-configured Deployment on every such call. Mirrors
		// ensureEnvironmentEnv's own cfg.Env["GOOSE_PROVIDER"] == "" guard.
		return nil
	}
	depClient := m.clientset.AppsV1().Deployments(m.namespace)
	dep, err := depClient.Get(ctx, backendRef, metav1.GetOptions{})
	if err != nil {
		// Let the caller's own scaleDeployment (which already handles
		// apierrors.IsNotFound specially) surface this.
		return nil
	}
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return nil
	}
	container := &dep.Spec.Template.Spec.Containers[0]

	existing := make(map[string]string, len(container.Env))
	for _, ev := range container.Env {
		existing[ev.Name] = ev.Value
	}

	stale := false
	for _, key := range pacaInfraEnvKeys {
		if cfg.Env[key] != existing[key] {
			stale = true
			break
		}
	}
	if !stale {
		return nil
	}

	skip := make(map[string]bool, len(pacaInfraEnvKeys))
	for _, key := range pacaInfraEnvKeys {
		skip[key] = true
	}
	desired := make([]corev1.EnvVar, 0, len(container.Env)+len(pacaInfraEnvKeys))
	for _, ev := range container.Env {
		if skip[ev.Name] {
			continue
		}
		desired = append(desired, ev)
	}
	for _, key := range pacaInfraEnvKeys {
		if v := cfg.Env[key]; v != "" {
			desired = append(desired, corev1.EnvVar{Name: key, Value: v})
		}
	}
	container.Env = desired

	if _, err := depClient.Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("sandbox/k8s: update environment deployment %s to refresh infra env: %w", backendRef, err)
	}
	return nil
}

// StartEnvironment scales backendRef's existing Deployment back to 1
// replica and waits for its Pod — possibly a brand new one, since a
// stop/start cycle can land on a different Pod/IP than before, see
// StopEnvironment — to become ready. Does not create a new Deployment.
//
// Safe to call even when the Deployment is already at 1 replica:
// scaleDeployment's merge patch sets spec.replicas to the same value
// either way — a no-op that never touches spec.template, so it can't
// trigger a rolling restart of an already-running Pod — and
// waitForPodIP/WaitForReady below always re-resolve the Pod fresh from a
// live List/HTTP call rather than any cached address, so an
// already-running environment's current, already-healthy Pod is what
// gets returned, quickly.
//
// ensureEnvironmentEnv and ensureEnvironmentInfraEnv both run first — see
// their own doc comments for the two exceptions to "never touches
// spec.template": backfilling GOOSE_PROVIDER onto a Deployment that
// predates coldStartEnvironment ever computing it (a one-time patch,
// guarded by GOOSE_PROVIDER's own presence so it can never fire twice for
// the same Deployment), and keeping pacaInfraEnvKeys in sync with this
// process's own current config (not one-time — re-checked, and re-applied
// when stale, on every call).
func (m *Manager) StartEnvironment(ctx context.Context, backendRef string, cfg sandbox.EnvironmentConfig) (*sandbox.EnvironmentHandle, error) {
	if err := m.ensureEnvironmentEnv(ctx, backendRef, cfg); err != nil {
		return nil, err
	}
	if err := m.ensureEnvironmentInfraEnv(ctx, backendRef, cfg); err != nil {
		return nil, err
	}
	if err := m.scaleDeployment(ctx, backendRef, 1); err != nil {
		if apierrors.IsNotFound(err) {
			// The Deployment was removed outside of Paca (a manual
			// `kubectl delete`, or any cluster operation this Manager
			// wasn't the one to perform) — there is nothing to scale up.
			// Wrapped with sandbox.ErrEnvironmentGone for the same reason
			// the docker backend's StartEnvironment does — see that
			// method's matching branch and ErrEnvironmentGone's own doc
			// comment (internal/sandbox/environment.go).
			return nil, fmt.Errorf("sandbox/k8s: environment deployment %s no longer exists — it was likely removed outside of Paca; delete and recreate this environment: %w", backendRef, sandbox.ErrEnvironmentGone)
		}
		return nil, fmt.Errorf("sandbox/k8s: start environment: %w", err)
	}

	selector := environmentLabel + "=" + backendRef
	podName, podIP, err := m.waitForPodIP(ctx, selector)
	if err != nil {
		diag := m.diagnoseUnreadyEnvironment(context.Background(), backendRef)
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	baseURL, err := sandbox.WaitForReady(ctx, []string{fmt.Sprintf("http://%s:%d", podIP, sandbox.GooseServePort)}, cfg.SecretKey)
	if err != nil {
		diag := m.diagnoseUnreadyEnvironment(context.Background(), backendRef)
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	// A scale-0→1 always schedules a brand-new Pod (see this method's own
	// doc comment), so a fresh dind sidecar needs its own readiness check
	// here too, same as CreateEnvironment — nothing about an already-Ready
	// Pod from a prior Start is reused across a stop/start cycle.
	if cfg.DockerEnabled {
		if err := m.waitForDindReady(ctx, podName); err != nil {
			diag := m.diagnoseUnreadyEnvironment(context.Background(), backendRef)
			return nil, fmt.Errorf("sandbox/k8s: environment dind sidecar not ready: %w (%s)", err, diag)
		}
	}

	return &sandbox.EnvironmentHandle{BackendRef: backendRef, BaseURL: baseURL}, nil
}

// StopEnvironment scales backendRef's Deployment to 0 replicas —
// Kubernetes terminates the Pod, but the Deployment object and its PVC
// both persist untouched, so a later StartEnvironment finds the same
// container spec and disk state waiting for it.
func (m *Manager) StopEnvironment(ctx context.Context, backendRef string) error {
	if err := m.scaleDeployment(ctx, backendRef, 0); err != nil {
		// A Deployment that's already gone is, for StopEnvironment's
		// purposes, already stopped — treated as success. Matches
		// DeleteEnvironment's own already-established not-found tolerance
		// below and the docker backend's identical StopEnvironment
		// behavior; without this, an environment whose Deployment was
		// manually deleted would fail the idle reaper's StopEnvironment
		// call forever instead of settling into "stopped".
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// scaleDeployment sets a Deployment's spec.replicas via a JSON merge
// patch — StartEnvironment/StopEnvironment's shared primitive. A merge
// patch touches only spec.replicas, never spec.template, so re-patching
// replicas to its current value (StartEnvironment called against an
// already-running environment; see that method's own doc comment) is a
// true no-op: the Deployment controller only creates a new ReplicaSet
// (and, transitively, restarts Pods) when the Pod *template* changes, not
// when replicas alone is re-patched to the same number.
func (m *Manager) scaleDeployment(ctx context.Context, name string, replicas int32) error {
	patch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, replicas))
	if _, err := m.clientset.AppsV1().Deployments(m.namespace).Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("sandbox/k8s: scale deployment %s to %d replicas: %w", name, replicas, err)
	}
	return nil
}

// DeleteEnvironment permanently deletes backendRef's Deployment (cascading
// to its Pod) and volumeRef's PersistentVolumeClaim — the only
// irreversible teardown in this file, matching
// EnvironmentBackend.DeleteEnvironment's own doc comment. Both deletes are
// attempted even if one fails, so a caller retrying after a partial
// failure can still clean up whichever object is left; errors from either
// are combined rather than the second delete being skipped on the first
// one's failure.
func (m *Manager) DeleteEnvironment(ctx context.Context, backendRef, volumeRef string) error {
	depErr := m.deleteDeployment(ctx, backendRef)

	// The environment's NodePort Service (if any — ensureEnvironmentService
	// never creates one for an environment with no configured/assigned
	// ports at all) must be explicitly deleted too: unlike the Deployment's
	// Pod, nothing else ever cascades its removal, and an orphaned Service
	// would otherwise hold its NodePort(s) forever.
	svcErr := m.ensureEnvironmentService(ctx, backendRef, nil, nil)

	pvcErr := m.clientset.CoreV1().PersistentVolumeClaims(m.namespace).Delete(ctx, volumeRef, metav1.DeleteOptions{})
	if pvcErr != nil {
		if apierrors.IsNotFound(pvcErr) {
			pvcErr = nil
		} else {
			pvcErr = fmt.Errorf("sandbox/k8s: delete environment pvc %s: %w", volumeRef, pvcErr)
		}
	}

	return errors.Join(depErr, svcErr, pvcErr)
}

// deleteDeployment issues Foreground-propagating deletion of an
// environment's Deployment (cascading to its Pod) — CreateEnvironment's
// own failure-cleanup path and DeleteEnvironment both need this. Mirrors
// deleteJob's stance in manager.go on an already-gone object: not an
// error, since a caller retrying after a partial failure may race with a
// previous attempt's own cleanup.
func (m *Manager) deleteDeployment(ctx context.Context, name string) error {
	propagation := metav1.DeletePropagationForeground
	err := m.clientset.AppsV1().Deployments(m.namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("sandbox/k8s: delete environment deployment %s: %w", name, err)
	}
	return nil
}

// podNameForEnvironment resolves backendRef (a Deployment name) to its
// current Pod's name, read fresh on every call — the same "never cache,
// the address can change across a stop/start cycle" stance podNameForJob
// (manager.go) takes for a Job's Pod, applied to a Deployment instead.
// Selects on environmentLabel rather than any Kubernetes-automatic label
// — see that constant's own doc comment for why a Deployment needs one at
// all, unlike a Job.
//
// Uses selectEnvironmentPod rather than blindly taking whichever Pod the
// List call happens to return first: a Deployment update (e.g.
// ensureEnvironmentEnv's one-time GOOSE_PROVIDER backfill, which issues an
// Update that rolls the Deployment) can transiently leave two Pods sharing
// this same label — the old one terminating, the new one starting — and
// list order isn't guaranteed, so an exec/copy/attach routed through the
// wrong one could target the terminating Pod and fail mid-operation.
func (m *Manager) podNameForEnvironment(ctx context.Context, backendRef string) (string, error) {
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: environmentLabel + "=" + backendRef})
	if err != nil {
		return "", fmt.Errorf("sandbox/k8s: list pods for environment %s: %w", backendRef, err)
	}
	pod, ok := selectEnvironmentPod(pods.Items)
	if !ok {
		return "", fmt.Errorf("sandbox/k8s: no running pod found for environment %s", backendRef)
	}
	return pod.Name, nil
}

// selectEnvironmentPod picks the one Pod podNameForEnvironment's caller
// should actually talk to, out of every Pod currently matching an
// environment's label selector — deterministic even mid-rollout, unlike
// blindly taking index 0 (see podNameForEnvironment's own doc comment for
// why that's not safe). Excludes anything terminating
// (DeletionTimestamp set) or not yet Running, then returns the most
// recently created of what's left: during a rollout the newest Pod is the
// one the Deployment is converging toward, not the one on its way out.
func selectEnvironmentPod(pods []corev1.Pod) (corev1.Pod, bool) {
	var best corev1.Pod
	found := false
	for _, p := range pods {
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning {
			continue
		}
		if !found || best.CreationTimestamp.Before(&p.CreationTimestamp) {
			best = p
			found = true
		}
	}
	return best, found
}

// CopyToEnvironment uploads tarContent into backendRef (a Deployment
// name)'s current Pod at destPath — the environment counterpart to
// CopyToContainer, resolving the Pod fresh via podNameForEnvironment
// first. See CopyToContainer's own doc comment in manager.go for the
// tar-over-stdin mechanism, reused verbatim via streamExec.
func (m *Manager) CopyToEnvironment(ctx context.Context, backendRef, destPath string, tarContent io.Reader) error {
	podName, err := m.podNameForEnvironment(ctx, backendRef)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := []string{"tar", "-xf", "-", "-C", destPath}
	if err := m.streamExec(ctx, podName, containerName, cmd, tarContent, io.Discard, &stderr); err != nil {
		return fmt.Errorf("sandbox/k8s: copy to environment pod %s: %w (stderr: %s)", podName, err, stderr.String())
	}
	return nil
}

// ExecEnvironment runs cmd inside backendRef's current Pod and returns its
// combined stdout+stderr and exit code — the environment counterpart to
// Exec (manager.go). Same err-vs-exitCode contract as Exec: err is for
// infrastructure failures (resolving the Pod, establishing the exec
// stream), not for cmd itself exiting non-zero.
func (m *Manager) ExecEnvironment(ctx context.Context, backendRef string, cmd []string) (output string, exitCode int, err error) {
	podName, err := m.podNameForEnvironment(ctx, backendRef)
	if err != nil {
		return "", 0, err
	}
	var buf bytes.Buffer
	execErr := m.streamExec(ctx, podName, containerName, cmd, nil, &buf, &buf)
	if execErr == nil {
		return buf.String(), 0, nil
	}
	if code, ok := exitCodeFromExecErr(execErr); ok {
		return buf.String(), code, nil
	}
	return buf.String(), 0, fmt.Errorf("sandbox/k8s: exec in environment pod %s: %w", podName, execErr)
}

// StreamExecEnvironment runs an interactive PTY session (cmd, typically
// ["/bin/bash"]) inside backendRef's current Pod for the browser terminal
// — the environment counterpart to Exec, but interactive/long-lived
// instead of one-shot. Builds its own pods/exec request rather than
// reusing streamExec (manager.go), which hardcodes TTY:false and has no
// resize support — the underlying SPDY-executor plumbing
// (remotecommand.NewSPDYExecutor, m.restConfig) is the same mechanism
// either way.
//
// With TTY true, a real PTY has no separate stderr channel, and the
// Kubernetes API rejects a PodExecOptions that sets both TTY and Stderr —
// so PodExecOptions.Stderr is always false here, and stdout carries
// everything the process writes to either stream; the stderr parameter is
// accepted (to satisfy EnvironmentBackend's fixed signature) but never
// written to. Blocks until cmd exits, the connection breaks, or ctx is
// cancelled, per StreamWithContext's own contract — the same one
// streamExec relies on.
func (m *Manager) StreamExecEnvironment(ctx context.Context, backendRef string, cmd []string, stdin io.Reader, stdout, stderr io.Writer, resize <-chan sandbox.TermSize) error {
	podName, err := m.podNameForEnvironment(ctx, backendRef)
	if err != nil {
		return err
	}

	req := m.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(m.namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    false,
			TTY:       true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(m.restConfig, http.MethodPost, req.URL())
	if err != nil {
		return fmt.Errorf("sandbox/k8s: build stream exec request: %w", err)
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Tty:               true,
		TerminalSizeQueue: &resizeQueue{ctx: ctx, resize: resize},
	})
}

// resizeQueue adapts a <-chan sandbox.TermSize into
// remotecommand.TerminalSizeQueue — the interface client-go's SPDY
// executor polls (over the exec stream's own dedicated resize channel)
// for PTY dimension changes, the same mechanism kubectl exec -it uses for
// its own resize support.
type resizeQueue struct {
	ctx    context.Context
	resize <-chan sandbox.TermSize
}

// Next blocks for the next resize event and translates it to
// remotecommand.TerminalSize. Returns nil — client-go's own signal to
// stop watching for further resizes — once resize closes or ctx is done,
// matching StreamExecEnvironment's documented "stops watching resize when
// ctx is done or the command exits" contract (see EnvironmentBackend's
// own doc comment in ../environment.go).
func (q *resizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case size, ok := <-q.resize:
		if !ok {
			return nil
		}
		return &remotecommand.TerminalSize{Width: size.Cols, Height: size.Rows}
	case <-q.ctx.Done():
		return nil
	}
}

// diagnoseUnreadyEnvironment mirrors diagnoseUnready in manager.go —
// summarizes the current Pod state and recent container logs for
// CreateEnvironment/StartEnvironment's error paths — parameterized by
// environmentLabel's selector instead of a Job's automatic job-name one.
// Kept as its own copy rather than generalizing diagnoseUnready itself:
// the two differ only in the selector/log-message wording, not worth
// threading a shared selector-string parameter through Start's own call
// site for.
func (m *Manager) diagnoseUnreadyEnvironment(ctx context.Context, backendRef string) string {
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: environmentLabel + "=" + backendRef})
	if err != nil {
		return fmt.Sprintf("environment %s: list pods failed: %v", backendRef, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Sprintf("environment %s: no pod found", backendRef)
	}

	pod := pods.Items[0]
	state := fmt.Sprintf("pod=%s phase=%s", pod.Name, pod.Status.Phase)
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != containerName {
			continue
		}
		switch {
		case cs.State.Waiting != nil:
			state += fmt.Sprintf(" waiting=%s(%s)", cs.State.Waiting.Reason, cs.State.Waiting.Message)
		case cs.State.Terminated != nil:
			state += fmt.Sprintf(" terminated=%s exitCode=%d", cs.State.Terminated.Reason, cs.State.Terminated.ExitCode)
		}
	}

	logs := "(unavailable)"
	logsReq := m.clientset.CoreV1().Pods(m.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: containerName, TailLines: ptr.To(int64(40))})
	if rc, err := logsReq.Stream(ctx); err == nil {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rc)
		_ = rc.Close()
		if buf.Len() > 0 {
			logs = buf.String()
		} else {
			logs = "(empty)"
		}
	}

	return fmt.Sprintf("%s; last logs: %s", state, logs)
}
