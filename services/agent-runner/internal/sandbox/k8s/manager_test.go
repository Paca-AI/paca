package k8s

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	utilexec "k8s.io/utils/exec"
)

// Unlike ../sandbox_test.go's Docker-backed Manager, Start/Stop/
// CopyToContainer/Exec here ultimately need a real exec/attach stream a
// fake clientset can't provide (its RESTClient has no live HTTP server
// behind pods/exec) — so, matching that file's own precedent (only its
// pure helpers, selectContainerIP/imageConfirmed, get unit tests; Start/
// Stop/Exec are covered by test/e2e instead), these tests exercise this
// package's own pure/list-only logic: name derivation, Pod-resolution
// polling against the fake clientset's object tracker (which List/Watch
// do support), and the exit-code-unwrapping helper. Full Start/Stop
// coverage needs a real cluster, same as the Docker backend needs a real
// daemon.

func TestJobName_IsDeterministicAndPrefixed(t *testing.T) {
	const convID = "11111111-2222-3333-4444-555555555555"
	got := jobName(convID)
	want := "paca-sbx-" + convID
	if got != want {
		t.Errorf("jobName(%q) = %q, want %q", convID, got, want)
	}
	if got2 := jobName(convID); got2 != got {
		t.Errorf("jobName is not deterministic: got %q then %q for the same input", got, got2)
	}
}

func TestJobName_DifferentConversationsGetDifferentNames(t *testing.T) {
	a := jobName("11111111-2222-3333-4444-555555555555")
	b := jobName("66666666-7777-8888-9999-000000000000")
	if a == b {
		t.Errorf("jobName produced the same name for two different conversation IDs: %q", a)
	}
}

func managerWithPods(namespace string, pods ...*corev1.Pod) *Manager {
	clientset := fake.NewClientset()
	for _, p := range pods {
		if _, err := clientset.CoreV1().Pods(namespace).Create(context.Background(), p, metav1.CreateOptions{}); err != nil {
			panic(err)
		}
	}
	return &Manager{clientset: clientset, namespace: namespace}
}

func podFixture(name, jobLabel string, phase corev1.PodPhase, ip string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"job-name": jobLabel},
		},
		Status: corev1.PodStatus{Phase: phase, PodIP: ip},
	}
}

func TestWaitForPodIP_ReturnsAssignedIP(t *testing.T) {
	const job = "paca-sbx-conv1"
	m := managerWithPods("paca", podFixture("paca-sbx-conv1-x1", job, corev1.PodRunning, "10.0.0.5"))

	podName, podIP, err := m.waitForPodIP(context.Background(), "job-name="+job)
	if err != nil {
		t.Fatalf("waitForPodIP: %v", err)
	}
	if podName != "paca-sbx-conv1-x1" {
		t.Errorf("podName = %q, want %q", podName, "paca-sbx-conv1-x1")
	}
	if podIP != "10.0.0.5" {
		t.Errorf("podIP = %q, want %q", podIP, "10.0.0.5")
	}
}

func TestWaitForPodIP_FailsFastOnFailedPhase(t *testing.T) {
	const job = "paca-sbx-conv2"
	m := managerWithPods("paca", podFixture("paca-sbx-conv2-x1", job, corev1.PodFailed, ""))

	_, _, err := m.waitForPodIP(context.Background(), "job-name="+job)
	if err == nil {
		t.Fatal("waitForPodIP: expected an error for a Failed pod, got nil")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("waitForPodIP error = %q, want it to mention the pod failed", err.Error())
	}
}

func TestWaitForPodIP_ReturnsContextErrorWhenCancelled(t *testing.T) {
	const job = "paca-sbx-conv3"
	m := managerWithPods("paca") // no pods at all — every List comes back empty

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := m.waitForPodIP(ctx, "job-name="+job)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("waitForPodIP with an already-cancelled context = %v, want context.Canceled", err)
	}
}

// TestWaitForPodIP_SkipsTerminatingPodAndFindsReplacement is the regression
// test for a live bug: StopEnvironment's scale-to-0 patch returns success
// as soon as it's accepted, without waiting for the old Pod to actually
// finish terminating (terminationGracePeriodSeconds means that can take
// real time) — a Start called immediately after can have waitForPodIP's
// List() see both the still-terminating old Pod (which still carries a
// non-empty PodIP right up until it's actually gone) and the fresh
// replacement. Before this fix, whichever sorted first in the list
// response (unspecified order) won, sometimes handing back an address
// that was already on its way out.
func TestWaitForPodIP_SkipsTerminatingPodAndFindsReplacement(t *testing.T) {
	const job = "paca-sbx-conv5"
	terminating := podFixture("paca-sbx-conv5-old", job, corev1.PodRunning, "10.0.0.9")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	fresh := podFixture("paca-sbx-conv5-new", job, corev1.PodRunning, "10.0.0.10")

	m := managerWithPods("paca", terminating, fresh)

	podName, podIP, err := m.waitForPodIP(context.Background(), "job-name="+job)
	if err != nil {
		t.Fatalf("waitForPodIP: %v", err)
	}
	if podName != "paca-sbx-conv5-new" {
		t.Errorf("podName = %q, want the fresh replacement %q, not the terminating Pod", podName, "paca-sbx-conv5-new")
	}
	if podIP != "10.0.0.10" {
		t.Errorf("podIP = %q, want %q", podIP, "10.0.0.10")
	}
}

// TestWaitForPodIP_ErrorsWhenOnlyMatchIsTerminating confirms a terminating
// Pod is never an acceptable answer even when it's the only match — the
// caller should keep polling (and eventually time out) rather than hand
// back an address on its way out.
func TestWaitForPodIP_ErrorsWhenOnlyMatchIsTerminating(t *testing.T) {
	const job = "paca-sbx-conv6"
	terminating := podFixture("paca-sbx-conv6-old", job, corev1.PodRunning, "10.0.0.9")
	now := metav1.Now()
	terminating.DeletionTimestamp = &now

	m := managerWithPods("paca", terminating)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := m.waitForPodIP(ctx, "job-name="+job)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waitForPodIP with only a terminating Pod matching = %v, want context.DeadlineExceeded (never returns the terminating Pod's address)", err)
	}
}

func TestPodNameForJob_ResolvesToTheJobsPod(t *testing.T) {
	const job = "paca-sbx-conv4"
	m := managerWithPods("paca", podFixture("paca-sbx-conv4-abcde", job, corev1.PodRunning, "10.0.0.9"))

	got, err := m.podNameForJob(context.Background(), job)
	if err != nil {
		t.Fatalf("podNameForJob: %v", err)
	}
	if got != "paca-sbx-conv4-abcde" {
		t.Errorf("podNameForJob = %q, want %q", got, "paca-sbx-conv4-abcde")
	}
}

func TestPodNameForJob_ErrorsWhenNoPodExists(t *testing.T) {
	m := managerWithPods("paca")

	if _, err := m.podNameForJob(context.Background(), "paca-sbx-missing"); err == nil {
		t.Error("podNameForJob: expected an error when no pod matches, got nil")
	}
}

func TestExitCodeFromExecErr_UnwrapsCodeExitError(t *testing.T) {
	wrapped := fmt.Errorf("exec failed: %w", utilexec.CodeExitError{Err: errors.New("command terminated with exit code 3"), Code: 3})

	code, ok := exitCodeFromExecErr(wrapped)
	if !ok {
		t.Fatal("exitCodeFromExecErr: expected ok=true for a wrapped CodeExitError")
	}
	if code != 3 {
		t.Errorf("exitCodeFromExecErr code = %d, want 3", code)
	}
}

func TestExitCodeFromExecErr_FalseForUnrelatedError(t *testing.T) {
	_, ok := exitCodeFromExecErr(errors.New("connection refused"))
	if ok {
		t.Error("exitCodeFromExecErr: expected ok=false for an error that isn't a CodeExitError")
	}
}

func TestSandboxSecurityContext_DropsAllAndAddsOnlyNarrowSet(t *testing.T) {
	sc := sandboxSecurityContext()
	if sc == nil || sc.Capabilities == nil {
		t.Fatal("sandboxSecurityContext: expected a non-nil Capabilities block")
	}
	if len(sc.Capabilities.Drop) != 1 || sc.Capabilities.Drop[0] != "ALL" {
		t.Errorf("sandboxSecurityContext Drop = %v, want [\"ALL\"]", sc.Capabilities.Drop)
	}
	want := map[corev1.Capability]bool{"CHOWN": true, "SETUID": true, "SETGID": true, "DAC_OVERRIDE": true, "FOWNER": true}
	if len(sc.Capabilities.Add) != len(want) {
		t.Errorf("sandboxSecurityContext Add = %v, want exactly %v", sc.Capabilities.Add, want)
	}
	for _, cap := range sc.Capabilities.Add {
		if !want[cap] {
			t.Errorf("sandboxSecurityContext Add contains unexpected capability %q", cap)
		}
	}
	if sc.RunAsNonRoot != nil && *sc.RunAsNonRoot {
		t.Error("sandboxSecurityContext must not force RunAsNonRoot — the sandbox needs to run as root")
	}
}

func TestDindContainer_IsPrivilegedAndUsesPinnedImage(t *testing.T) {
	limits := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")},
	}
	c := dindContainer(limits)
	if c.Name != dindContainerName {
		t.Errorf("dindContainer name = %q, want %q", c.Name, dindContainerName)
	}
	if c.SecurityContext == nil || c.SecurityContext.Privileged == nil || !*c.SecurityContext.Privileged {
		t.Error("dindContainer must run privileged — dockerd cannot manage cgroups/mounts/iptables otherwise")
	}
	if c.Image == "" {
		t.Error("dindContainer has no image set")
	}
	if c.Resources.Limits.Cpu().IsZero() || c.Resources.Limits.Memory().IsZero() {
		t.Error("dindContainer has no resource limits set — an unbounded privileged sidecar can starve its node")
	}
}

func TestDiagnoseUnready_ReportsPodNameAndPhaseWithoutPanicking(t *testing.T) {
	const job = "paca-sbx-conv5"
	m := managerWithPods("paca", podFixture("paca-sbx-conv5-zz", job, corev1.PodPending, ""))

	got := m.diagnoseUnready(context.Background(), job)
	if !strings.Contains(got, "paca-sbx-conv5-zz") {
		t.Errorf("diagnoseUnready = %q, want it to mention the pod name", got)
	}
	if !strings.Contains(got, "Pending") {
		t.Errorf("diagnoseUnready = %q, want it to mention the pod phase", got)
	}
}

func TestDiagnoseUnready_ReportsWhenNoPodFound(t *testing.T) {
	m := managerWithPods("paca")

	got := m.diagnoseUnready(context.Background(), "paca-sbx-missing")
	if !strings.Contains(got, "no pod found") {
		t.Errorf("diagnoseUnready = %q, want it to say no pod was found", got)
	}
}
