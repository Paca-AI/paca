// Package k8s implements sandbox.Backend by running each conversation's
// sandbox as a Kubernetes Job instead of a Docker container — the backend
// selected when agent-runner itself runs inside a Kubernetes cluster (see
// internal/config's SANDBOX_BACKEND), where there is no host Docker socket
// to mount the way ../sandbox.go's Docker-based Manager does.
//
// One Job per conversation, one Pod per Job (BackoffLimit 0 — this package
// owns the sandbox's lifecycle itself, the same stance ../sandbox.go takes,
// so a crash should surface as an error, not a silent Kubernetes-level
// retry). CopyToContainer/Exec reach the Pod through the pods/exec
// subresource — the same mechanism `kubectl cp`/`kubectl exec` use — since
// that's the only way to reach a container's filesystem/process from
// outside its own Pod, in contrast to Docker's sibling-container mode
// which reaches the daemon directly over a mounted socket.
package k8s

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/utils/exec"
	"k8s.io/utils/ptr"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

const (
	// containerName is the primary sandbox container inside every Job's
	// Pod — CopyToContainer/Exec always target this one explicitly, never
	// the "dind" sidecar (see dind.go) that may also be running alongside
	// it.
	containerName = "agent-server"

	// labelConvID/labelManaged mirror ../sandbox.go's own Docker labels —
	// same names in spirit, same purpose (identify/filter this package's
	// own managed resources), just spelled as valid Kubernetes label keys.
	labelConvID  = "paca.ai/conversation-id"
	labelManaged = "paca.ai/managed-by"
	managedValue = "agent-runner"

	// podReadyTimeout bounds how long Start waits for the Job's Pod to
	// exist and report a PodIP before giving up — separate from
	// sandbox.WaitForReady's own timeout (which only starts once a
	// candidate address exists at all), so a Pod that never schedules
	// (e.g. no node has capacity) fails with its own clear error instead
	// of being silently folded into WaitForReady's "never became ready"
	// message.
	podReadyTimeout   = 60 * time.Second
	podReadyPollEvery = 250 * time.Millisecond

	// jobTTLSecondsAfterFinished is a leak-safety net mirroring the
	// Docker backend's AutoRemove: true — Stop already deletes the Job
	// explicitly on every path, but this ensures a Job this process never
	// got the chance to delete (e.g. a crash between Start and Stop) is
	// still garbage-collected by Kubernetes itself rather than lingering
	// forever.
	jobTTLSecondsAfterFinished = int32(300)

	// defaultCPULimit/defaultMemoryLimit match ../sandbox.go's own
	// hardcoded container.Resources in its Start — kept in sync by hand
	// since the two backends use entirely different resource-spec types
	// (Docker's NanoCPUs/Memory vs. Kubernetes' resource.Quantity), not a
	// shared constant.
	defaultCPULimit    = "2"
	defaultMemoryLimit = "4Gi"
)

// Options configures a Manager — the Kubernetes-specific knobs
// internal/config.Settings exposes for SANDBOX_BACKEND=kubernetes.
type Options struct {
	// Namespace is where every sandbox Job/Pod is created. Required.
	Namespace string
	// CPULimit/MemoryLimit set both the request and the limit on the
	// primary sandbox container, in resource.ParseQuantity syntax (e.g.
	// "2", "4Gi"). Empty means defaultCPULimit/defaultMemoryLimit.
	CPULimit    string
	MemoryLimit string
	// ImagePullSecrets names Secrets (of type kubernetes.io/dockerconfigjson)
	// already present in Namespace, attached to every sandbox Pod — needed
	// when the sandbox image is pulled from a private registry.
	ImagePullSecrets []string

	// AgentServerImage is config.Settings.AgentServerImage — the same
	// pinned image ephemeral conversation sandboxes run. Unlike Start,
	// which always receives an already-resolved cfg.Image from its caller
	// (executor.go) and never reads this field, environment.go's
	// CreateEnvironment/StartEnvironment fall back to it themselves
	// whenever EnvironmentConfig.Image is empty — see that type's own doc
	// comment in ../environment.go for why the fallback has to live in
	// this package rather than a caller that only resolves it once.
	AgentServerImage string
	// EnvironmentsStorageClassName sets StorageClassName on every
	// PersistentVolumeClaim environment.go's CreateEnvironment provisions
	// (SANDBOX_ENVIRONMENTS_STORAGE_CLASS). Empty leaves it unset, which
	// is not the same as "" — a nil StorageClassName is what actually
	// tells Kubernetes to provision from the cluster's own default
	// StorageClass, rather than this package hardcoding one that may not
	// exist on every cluster.
	EnvironmentsStorageClassName string
}

// Manager implements sandbox.Backend by creating one Kubernetes Job per
// conversation. clientset is kubernetes.Interface, not the concrete
// *kubernetes.Clientset, so tests can substitute
// k8s.io/client-go/kubernetes/fake instead of a real cluster.
type Manager struct {
	clientset  kubernetes.Interface
	restConfig *rest.Config
	namespace  string

	cpuLimit    resource.Quantity
	memoryLimit resource.Quantity

	imagePullSecrets []corev1.LocalObjectReference

	// agentServerImage/environmentsStorageClassName back environment.go's
	// CreateEnvironment/StartEnvironment defaulting only — Start itself
	// never reads either: cfg.Image is always caller-resolved for an
	// ephemeral sandbox (see sandbox.Config's own doc comment), and disk
	// sizing is per-environment (EnvironmentConfig.DiskLimitGB), not a
	// process-wide setting the way cpuLimit/memoryLimit above are.
	agentServerImage             string
	environmentsStorageClassName string
}

var _ sandbox.Backend = (*Manager)(nil)

// Manager also implements sandbox.EnvironmentBackend (see environment.go),
// satisfying FullBackend on the same struct — main.go widens its
// newSandboxBackend return type to FullBackend to expose both.
var _ sandbox.FullBackend = (*Manager)(nil)

// NewManager builds a Manager from Options and the standard in-cluster
// Kubernetes config (the ServiceAccount token/CA cert Kubernetes mounts
// into every Pod) — this process is expected to run as a Pod itself when
// SANDBOX_BACKEND=kubernetes, the same assumption rest.InClusterConfig
// itself documents.
func NewManager(opts Options) (*Manager, error) {
	if opts.Namespace == "" {
		return nil, errors.New("sandbox/k8s: Namespace is required")
	}

	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: build clientset: %w", err)
	}

	cpuLimit := opts.CPULimit
	if cpuLimit == "" {
		cpuLimit = defaultCPULimit
	}
	memoryLimit := opts.MemoryLimit
	if memoryLimit == "" {
		memoryLimit = defaultMemoryLimit
	}
	cpuQty, err := resource.ParseQuantity(cpuLimit)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: parse CPU limit %q: %w", cpuLimit, err)
	}
	memQty, err := resource.ParseQuantity(memoryLimit)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: parse memory limit %q: %w", memoryLimit, err)
	}

	pullSecrets := make([]corev1.LocalObjectReference, 0, len(opts.ImagePullSecrets))
	for _, name := range opts.ImagePullSecrets {
		pullSecrets = append(pullSecrets, corev1.LocalObjectReference{Name: name})
	}

	return &Manager{
		clientset:                    clientset,
		restConfig:                   restConfig,
		namespace:                    opts.Namespace,
		cpuLimit:                     cpuQty,
		memoryLimit:                  memQty,
		imagePullSecrets:             pullSecrets,
		agentServerImage:             opts.AgentServerImage,
		environmentsStorageClassName: opts.EnvironmentsStorageClassName,
	}, nil
}

// jobName derives a deterministic, DNS-1123-safe Job name from a
// conversation ID — conversation IDs are UUIDs (see executor.go's
// trigger.ConversationID.String()), already lowercase hex + hyphens, so no
// further sanitizing is needed beyond the prefix. Deterministic (not
// Kubernetes-assigned) so Stop/CopyToContainer/Exec can address the same
// Job by reconstructing this name from Handle.ContainerID, without this
// package having to track any state of its own between calls.
func jobName(conversationID string) string {
	return "paca-sbx-" + conversationID
}

// sandboxSecurityContext is the Linux-capabilities hardening applied to
// every non-privileged sandbox container — Start's Job container below and
// environment.go's Deployment container alike (the dind sidecar is
// deliberately excluded from both: it needs Privileged: true, which
// overrides/ignores any capability list anyway — see dind.go's own
// comment). Root stays the default user either way — see the callers'
// own comments on why a blanket non-root flip isn't the answer here — but
// every capability not explicitly needed by that root user is dropped,
// with a narrow allow-list added back for the filesystem-ownership
// operations (package installs writing outside a plain user's own files,
// git/chown edge cases across an arbitrary checked-out repo, etc.) that
// running as root inside the sandbox is actually relied on for.
//
// A per-Pod PID limit (a cheap fork-bomb bound, the other half of the
// hardening pass described in docs/ai-agent/environment-management.md)
// has no equivalent field here: unlike Docker's per-container PidsLimit,
// Kubernetes only exposes this as a kubelet/node-level setting
// (--pod-max-pids, or podPidsLimit in the KubeletConfiguration) that
// applies uniformly to every Pod on a node — there is no per-Pod-spec API
// to set it from this package, so nothing is invented here to fake one.
func sandboxSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
			Add: []corev1.Capability{
				"CHOWN", "SETUID", "SETGID", "DAC_OVERRIDE", "FOWNER",
			},
		},
	}
}

// environmentSecurityContext is sandboxSecurityContext's own capability
// list plus SYS_CHROOT and AUDIT_WRITE, applied only to a static
// environment's Deployment container (environment.go) — never to Start's
// ephemeral per-conversation Job container above, which never runs real
// sshd. Mirrors internal/sandbox/docker/environment.go's own
// createAndStartEnvironmentContainer, which adds the exact same two
// capabilities with the exact same "environment container only" scoping —
// see that file's doc comment for the live-confirmed reasoning behind each
// one (sshd's privilege-separated pre-auth chroot, and its post-auth
// login/session accounting). Kept as a distinct function rather than a
// parameter on sandboxSecurityContext so the ephemeral-sandbox call site
// can't accidentally acquire these by a careless future edit.
func environmentSecurityContext() *corev1.SecurityContext {
	ctx := sandboxSecurityContext()
	ctx.Capabilities.Add = append(ctx.Capabilities.Add, "SYS_CHROOT", "AUDIT_WRITE")
	return ctx
}

// Start creates a Job for cfg.ConversationID and waits for its Pod to
// answer goose serve's /status endpoint — the Kubernetes analog of
// Manager.Start in ../sandbox.go; see that method's doc comment for the
// env-var/GitCommitter contract this mirrors exactly.
func (m *Manager) Start(ctx context.Context, cfg sandbox.Config) (*sandbox.Handle, error) {
	if cfg.MCPDevSourceDir != "" {
		return nil, errors.New("sandbox/k8s: MCPDevSourceDir is not supported by the kubernetes backend — " +
			"it bind-mounts a path from the host running agent-runner, which has no equivalent across a " +
			"multi-node cluster; use the docker backend for local apps/mcp development instead")
	}

	secretKey, err := sandbox.RandomHex(32)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: generate secret key: %w", err)
	}

	name := jobName(cfg.ConversationID)
	labels := map[string]string{
		labelConvID:  cfg.ConversationID,
		labelManaged: managedValue,
	}

	// User-configured env vars first, so the hardcoded infra vars below
	// always win on a name collision — same ordering rationale as
	// ../sandbox.go's Start.
	env := make([]corev1.EnvVar, 0, len(cfg.Env)+6)
	for k, v := range cfg.Env {
		env = append(env, corev1.EnvVar{Name: k, Value: v})
	}
	env = append(env,
		corev1.EnvVar{Name: "GOOSE_SERVER__SECRET_KEY", Value: secretKey},
		corev1.EnvVar{Name: "GIT_AUTHOR_NAME", Value: cfg.GitCommitterName},
		corev1.EnvVar{Name: "GIT_AUTHOR_EMAIL", Value: cfg.GitCommitterEmail},
		corev1.EnvVar{Name: "GIT_COMMITTER_NAME", Value: cfg.GitCommitterName},
		corev1.EnvVar{Name: "GIT_COMMITTER_EMAIL", Value: cfg.GitCommitterEmail},
	)
	if cfg.DockerEnabled {
		// Sidecar and primary container share one Pod's network
		// namespace (unlike ../dind.go's Docker backend, which needs a
		// dedicated per-conversation network to pair the two), so
		// "localhost" already reaches the dind sidecar directly — see
		// dind.go's doc comment.
		env = append(env, corev1.EnvVar{Name: "DOCKER_HOST", Value: "tcp://localhost:2375"})
	}

	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: m.cpuLimit, corev1.ResourceMemory: m.memoryLimit},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: m.cpuLimit, corev1.ResourceMemory: m.memoryLimit},
	}

	containers := []corev1.Container{{
		Name:      containerName,
		Image:     cfg.Image,
		Args:      []string{"serve", "--host", "0.0.0.0", "--port", strconv.Itoa(sandbox.GooseServePort)},
		Env:       env,
		Resources: resources,
		Ports:     []corev1.ContainerPort{{ContainerPort: int32(sandbox.GooseServePort)}},
		// SecurityContext below only narrows Linux capabilities (see
		// sandboxSecurityContext's own doc comment) — RunAsNonRoot is
		// deliberately still NOT set: the sandbox needs to run as root
		// (package installs, chown/chmod across arbitrary repo files, etc.
		// — the same reason services/agent-runner/Dockerfile itself runs
		// as root), so a namespace enforcing the Restricted Pod Security
		// Standard still rejects every sandbox Pod outright, not only
		// DockerEnabled ones — see deploy/helm/paca's own README. A
		// per-Pod PID limit (a cheap fork-bomb bound, the other half of
		// this hardening pass) has no Pod-spec-level field to set here —
		// see sandboxSecurityContext's doc comment for why.
		SecurityContext: sandboxSecurityContext(),
	}}
	if cfg.DockerEnabled {
		containers = append(containers, dindContainer(resources))
	}

	terminationGrace := int64(sandbox.StopTimeout.Seconds())
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.namespace, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            ptr.To(int32(0)),
			TTLSecondsAfterFinished: ptr.To(jobTTLSecondsAfterFinished),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:                 corev1.RestartPolicyNever,
					AutomountServiceAccountToken:  ptr.To(false),
					TerminationGracePeriodSeconds: &terminationGrace,
					ImagePullSecrets:              m.imagePullSecrets,
					Containers:                    containers,
				},
			},
		},
	}

	if _, err := m.clientset.BatchV1().Jobs(m.namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
		return nil, fmt.Errorf("sandbox/k8s: create job: %w", err)
	}

	cleanup := func() {
		removeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = m.deleteJob(removeCtx, name)
	}

	podName, podIP, err := m.waitForPodIP(ctx, "job-name="+name)
	if err != nil {
		diag := m.diagnoseUnready(context.Background(), name)
		cleanup()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	baseURL, err := sandbox.WaitForReady(ctx, []string{fmt.Sprintf("http://%s:%d", podIP, sandbox.GooseServePort)}, secretKey)
	if err != nil {
		diag := m.diagnoseUnready(context.Background(), name)
		cleanup()
		return nil, fmt.Errorf("%w (%s)", err, diag)
	}

	if cfg.DockerEnabled {
		if err := m.waitForDindReady(ctx, podName); err != nil {
			cleanup()
			return nil, fmt.Errorf("sandbox/k8s: dind sidecar not ready: %w", err)
		}
	}

	// ContainerID holds the Job name (deterministic, reconstructible from
	// a conversation ID alone) rather than the Pod name Kubernetes
	// actually assigned — see the package doc comment and jobName's own
	// doc comment for why.
	return &sandbox.Handle{ContainerID: name, BaseURL: baseURL, SecretKey: secretKey}, nil
}

// Stop deletes the Job previously created by Start, cascading to its Pod —
// best-effort, matching Manager.Stop's own contract in ../sandbox.go: a
// failure to delete is returned for the caller to log, not treated as
// fatal, since jobTTLSecondsAfterFinished (and, for a still-running Job,
// nothing — it just keeps running) is the backstop either way.
func (m *Manager) Stop(ctx context.Context, h *sandbox.Handle) error {
	return m.deleteJob(ctx, h.ContainerID)
}

// deleteJob issues the actual Job deletion Stop and Start's own cleanup
// path both need — Foreground propagation so the Pod is gone (not merely
// scheduled for deletion) by the time this returns, mirroring how
// ../sandbox.go's ContainerRemove(Force: true) synchronously removes the
// container it's tearing down. A already-gone Job (IsNotFound) is not an
// error — both callers may race with Kubernetes' own TTL controller.
func (m *Manager) deleteJob(ctx context.Context, name string) error {
	propagation := metav1.DeletePropagationForeground
	err := m.clientset.BatchV1().Jobs(m.namespace).Delete(ctx, name, metav1.DeleteOptions{PropagationPolicy: &propagation})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("sandbox/k8s: delete job %s: %w", name, err)
	}
	return nil
}

// waitForPodIP polls for a Pod matching selector (a full label-selector
// string, e.g. "job-name=paca-sbx-xyz" — Kubernetes labels every Pod a Job
// creates with job-name=<job name> automatically) until it has been
// assigned an IP — the earliest point sandbox.WaitForReady's own HTTP
// polling can even begin. Fails fast (rather than waiting out the full
// timeout) when the matching Pod reaches a terminal Failed phase, since
// that state can never self-resolve into a PodIP appearing.
//
// Shared between Start above (selector built from the Job's own
// automatic job-name label) and environment.go's CreateEnvironment/
// StartEnvironment (selector built from environmentLabel, which
// Kubernetes has no built-in equivalent of for a Deployment's Pods) —
// this function itself has no opinion on which label a caller selects by.
func (m *Manager) waitForPodIP(ctx context.Context, selector string) (podName, podIP string, err error) {
	deadline := time.Now().Add(podReadyTimeout)
	for time.Now().Before(deadline) {
		pods, listErr := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if listErr == nil && len(pods.Items) > 0 {
			pod := pods.Items[0]
			if pod.Status.Phase == corev1.PodFailed {
				return "", "", fmt.Errorf("sandbox/k8s: pod %s (selector %q) failed before becoming ready", pod.Name, selector)
			}
			if pod.Status.PodIP != "" {
				return pod.Name, pod.Status.PodIP, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(podReadyPollEvery):
		}
	}
	return "", "", fmt.Errorf("sandbox/k8s: no pod matching selector %q got an IP after %s", selector, podReadyTimeout)
}

// podNameForJob resolves a Job name (Handle.ContainerID) to its current
// Pod name, for CopyToContainer/Exec — looked up fresh on every call
// rather than cached on the Handle, so a stale Pod name is never used (see
// the package doc comment on why Handle.ContainerID holds the Job name,
// not the Pod name, in the first place).
func (m *Manager) podNameForJob(ctx context.Context, jobName string) (string, error) {
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil {
		return "", fmt.Errorf("sandbox/k8s: list pods for job %s: %w", jobName, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("sandbox/k8s: no pod found for job %s", jobName)
	}
	return pods.Items[0].Name, nil
}

// CopyToContainer uploads tarContent into containerID (a Job name) at
// destPath, via `tar -xf - -C <destPath>` piped over the pods/exec
// subresource's stdin — the same technique `kubectl cp` uses internally,
// since Kubernetes has no dedicated file-copy API of its own. Mirrors
// Manager.CopyToContainer in ../sandbox.go; see that method's doc comment
// for what this is used for (writing skill files and .goosehints before
// the ACP session starts).
func (m *Manager) CopyToContainer(ctx context.Context, containerID, destPath string, tarContent io.Reader) error {
	podName, err := m.podNameForJob(ctx, containerID)
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := []string{"tar", "-xf", "-", "-C", destPath}
	if err := m.streamExec(ctx, podName, containerName, cmd, tarContent, io.Discard, &stderr); err != nil {
		return fmt.Errorf("sandbox/k8s: copy to pod %s: %w (stderr: %s)", podName, err, stderr.String())
	}
	return nil
}

// Exec runs cmd inside containerID (a Job name)'s agent-server container
// and returns its combined stdout+stderr and exit code. Mirrors
// Manager.Exec in ../sandbox.go — same contract: err is for
// infrastructure failures (resolving the Pod, establishing the exec
// stream), not for cmd itself exiting non-zero, which is reported via
// exitCode with a nil err instead.
func (m *Manager) Exec(ctx context.Context, containerID string, cmd []string) (output string, exitCode int, err error) {
	podName, err := m.podNameForJob(ctx, containerID)
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
	return buf.String(), 0, fmt.Errorf("sandbox/k8s: exec in pod %s: %w", podName, execErr)
}

// exitCodeFromExecErr extracts a nonzero-exit process's exit code from the
// error remotecommand.Executor.StreamWithContext returns for it — pulled
// out of Exec as its own pure function so the errors.As unwrapping can be
// unit tested directly, without needing a real (or faked) exec stream.
// k8s.io/client-go/tools/remotecommand wraps the remote command's exit
// code in a k8s.io/utils/exec.CodeExitError (confirmed directly in that
// package's v4.go, the streaming protocol version this client negotiates)
// — the same type kubectl's own exec implementation unwraps for the
// identical reason.
func exitCodeFromExecErr(err error) (code int, ok bool) {
	var exitErr utilexec.CodeExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitStatus(), true
	}
	return 0, false
}

// streamExec runs cmd inside podName's named container over the
// pods/exec subresource, via client-go's SPDY remotecommand executor — the
// shared plumbing CopyToContainer, Exec, and dind.go's waitForDindReady
// all build on, parameterized by which container so a dind-readiness
// check can target the sidecar instead of containerName.
func (m *Manager) streamExec(ctx context.Context, podName, container string, cmd []string, stdin io.Reader, stdout, stderr io.Writer) error {
	req := m.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(m.namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   cmd,
			Stdin:     stdin != nil,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(m.restConfig, http.MethodPost, req.URL())
	if err != nil {
		return fmt.Errorf("sandbox/k8s: build exec request: %w", err)
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// diagnoseUnready summarizes the Job's Pod state and recent container logs
// for the error path when it never becomes ready — the Kubernetes analog
// of ../sandbox.go's diagnoseUnready. Best-effort: lookup/log failures are
// folded into the returned string rather than propagated, since this only
// ever augments an error the caller is already returning.
func (m *Manager) diagnoseUnready(ctx context.Context, jobName string) string {
	pods, err := m.clientset.CoreV1().Pods(m.namespace).List(ctx, metav1.ListOptions{LabelSelector: "job-name=" + jobName})
	if err != nil {
		return fmt.Sprintf("job %s: list pods failed: %v", jobName, err)
	}
	if len(pods.Items) == 0 {
		return fmt.Sprintf("job %s: no pod found", jobName)
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
