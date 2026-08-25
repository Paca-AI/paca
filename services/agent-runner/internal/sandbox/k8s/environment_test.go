package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/ptr"

	"github.com/Paca-AI/agent-runner/internal/sandbox"
)

// Same scoping precedent as manager_test.go's own doc comment: full
// CreateEnvironment/StartEnvironment coverage needs a real cluster (their
// readiness path ultimately does a real HTTP GET against a Pod IP, which
// a fake clientset can't serve), so these tests exercise this file's own
// pure/list-only logic instead — name derivation, image/disk defaulting,
// Pod-resolution polling against the fake clientset's object tracker,
// Deployment-replica patching, and the PTY resize-queue adapter.

func TestEnvironmentName_IsDeterministicAndPrefixed(t *testing.T) {
	const envID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	got := environmentName(envID)
	want := "paca-env-" + envID
	if got != want {
		t.Errorf("environmentName(%q) = %q, want %q", envID, got, want)
	}
	if got2 := environmentName(envID); got2 != got {
		t.Errorf("environmentName is not deterministic: got %q then %q for the same input", got, got2)
	}
}

func TestEnvironmentName_DifferentEnvironmentsGetDifferentNames(t *testing.T) {
	a := environmentName("11111111-2222-3333-4444-555555555555")
	b := environmentName("66666666-7777-8888-9999-000000000000")
	if a == b {
		t.Errorf("environmentName produced the same name for two different environment IDs: %q", a)
	}
}

func TestResolveEnvironmentImage_UsesCfgImageWhenSet(t *testing.T) {
	got := resolveEnvironmentImage("ghcr.io/paca-ai/custom:v1", "ghcr.io/paca-ai/default:latest")
	if got != "ghcr.io/paca-ai/custom:v1" {
		t.Errorf("resolveEnvironmentImage = %q, want the explicit cfg.Image", got)
	}
}

func TestResolveEnvironmentImage_FallsBackWhenEmpty(t *testing.T) {
	got := resolveEnvironmentImage("", "ghcr.io/paca-ai/default:latest")
	if got != "ghcr.io/paca-ai/default:latest" {
		t.Errorf("resolveEnvironmentImage = %q, want the fallback platform default", got)
	}
}

func TestResolveEnvironmentDiskLimitGB_UsesPositiveCfgValue(t *testing.T) {
	if got := resolveEnvironmentDiskLimitGB(25); got != 25 {
		t.Errorf("resolveEnvironmentDiskLimitGB(25) = %d, want 25", got)
	}
}

func TestResolveEnvironmentDiskLimitGB_FallsBackWhenZeroOrNegative(t *testing.T) {
	if got := resolveEnvironmentDiskLimitGB(0); got != defaultDiskLimitGB {
		t.Errorf("resolveEnvironmentDiskLimitGB(0) = %d, want defaultDiskLimitGB (%d)", got, defaultDiskLimitGB)
	}
	if got := resolveEnvironmentDiskLimitGB(-5); got != defaultDiskLimitGB {
		t.Errorf("resolveEnvironmentDiskLimitGB(-5) = %d, want defaultDiskLimitGB (%d)", got, defaultDiskLimitGB)
	}
}

func TestEnvironmentResources_FallsBackToManagerDefaults(t *testing.T) {
	m := &Manager{
		cpuLimit:    resource.MustParse("2"),
		memoryLimit: resource.MustParse("4Gi"),
	}
	resources, err := m.environmentResources(sandbox.EnvironmentConfig{})
	if err != nil {
		t.Fatalf("environmentResources: %v", err)
	}
	if resources.Limits.Cpu().String() != "2" {
		t.Errorf("environmentResources CPU = %s, want manager default 2", resources.Limits.Cpu().String())
	}
	if resources.Limits.Memory().String() != "4Gi" {
		t.Errorf("environmentResources memory = %s, want manager default 4Gi", resources.Limits.Memory().String())
	}
}

func TestEnvironmentResources_UsesPerEnvironmentOverride(t *testing.T) {
	m := &Manager{
		cpuLimit:    resource.MustParse("2"),
		memoryLimit: resource.MustParse("4Gi"),
	}
	resources, err := m.environmentResources(sandbox.EnvironmentConfig{CPULimit: "4", MemoryLimit: "8Gi"})
	if err != nil {
		t.Fatalf("environmentResources: %v", err)
	}
	if resources.Requests.Cpu().String() != "4" {
		t.Errorf("environmentResources CPU = %s, want overridden 4", resources.Requests.Cpu().String())
	}
	if resources.Requests.Memory().String() != "8Gi" {
		t.Errorf("environmentResources memory = %s, want overridden 8Gi", resources.Requests.Memory().String())
	}
}

func TestEnvironmentResources_ErrorsOnUnparsableOverride(t *testing.T) {
	m := &Manager{cpuLimit: resource.MustParse("2"), memoryLimit: resource.MustParse("4Gi")}
	if _, err := m.environmentResources(sandbox.EnvironmentConfig{CPULimit: "not-a-quantity"}); err == nil {
		t.Error("environmentResources: expected an error for an unparsable CPULimit override, got nil")
	}
}

func envPodFixture(name, envDeploymentLabel string, phase corev1.PodPhase, ip string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{environmentLabel: envDeploymentLabel},
		},
		Status: corev1.PodStatus{Phase: phase, PodIP: ip},
	}
}

func TestPodNameForEnvironment_ResolvesToTheDeploymentsPod(t *testing.T) {
	const backendRef = "paca-env-envA"
	m := managerWithPods("paca", envPodFixture("paca-env-envA-abcde", backendRef, corev1.PodRunning, "10.0.0.9"))

	got, err := m.podNameForEnvironment(context.Background(), backendRef)
	if err != nil {
		t.Fatalf("podNameForEnvironment: %v", err)
	}
	if got != "paca-env-envA-abcde" {
		t.Errorf("podNameForEnvironment = %q, want %q", got, "paca-env-envA-abcde")
	}
}

func TestPodNameForEnvironment_ErrorsWhenNoPodExists(t *testing.T) {
	m := managerWithPods("paca")

	if _, err := m.podNameForEnvironment(context.Background(), "paca-env-missing"); err == nil {
		t.Error("podNameForEnvironment: expected an error when no pod matches, got nil")
	}
}

// TestPodNameForEnvironment_SkipsTerminatingPodDuringRollout is a
// regression test for the exact transient-rollout case selectEnvironmentPod
// exists for: a Deployment update (e.g. ensureEnvironmentEnv's one-time
// GOOSE_PROVIDER backfill) can briefly leave both the old, terminating Pod
// and the new one sharing environmentLabel. List order isn't guaranteed,
// so this must actively filter the terminating one out rather than rely
// on happening to see the live one first.
func TestPodNameForEnvironment_SkipsTerminatingPodDuringRollout(t *testing.T) {
	const backendRef = "paca-env-envC"
	terminating := envPodFixture("paca-env-envC-old", backendRef, corev1.PodRunning, "10.0.0.1")
	deleting := metav1.NewTime(time.Now())
	terminating.DeletionTimestamp = &deleting
	live := envPodFixture("paca-env-envC-new", backendRef, corev1.PodRunning, "10.0.0.2")
	m := managerWithPods("paca", terminating, live)

	got, err := m.podNameForEnvironment(context.Background(), backendRef)
	if err != nil {
		t.Fatalf("podNameForEnvironment: %v", err)
	}
	if got != "paca-env-envC-new" {
		t.Errorf("podNameForEnvironment = %q, want the non-terminating pod %q", got, "paca-env-envC-new")
	}
}

// TestPodNameForEnvironment_PicksTheNewestRunningPodDuringRollout confirms
// the tie-break among multiple live-and-Running matches (both the old and
// new Pod briefly Running, neither terminating yet) deterministically
// picks the one the rollout is converging toward, not whichever the
// Kubernetes API happened to list first.
func TestPodNameForEnvironment_PicksTheNewestRunningPodDuringRollout(t *testing.T) {
	const backendRef = "paca-env-envD"
	older := envPodFixture("paca-env-envD-old", backendRef, corev1.PodRunning, "10.0.0.1")
	older.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))
	newer := envPodFixture("paca-env-envD-new", backendRef, corev1.PodRunning, "10.0.0.2")
	newer.CreationTimestamp = metav1.NewTime(time.Now())
	m := managerWithPods("paca", older, newer)

	got, err := m.podNameForEnvironment(context.Background(), backendRef)
	if err != nil {
		t.Fatalf("podNameForEnvironment: %v", err)
	}
	if got != "paca-env-envD-new" {
		t.Errorf("podNameForEnvironment = %q, want the newer pod %q", got, "paca-env-envD-new")
	}
}

// TestPodNameForEnvironment_ErrorsWhenOnlyMatchIsNotRunning guards against
// routing an exec/copy/attach at a Pod that's merely Pending (not yet
// running the actual container) — a clear "no pod found" error is more
// useful than one that looks resolved but fails on the very next call.
func TestPodNameForEnvironment_ErrorsWhenOnlyMatchIsNotRunning(t *testing.T) {
	const backendRef = "paca-env-envE"
	m := managerWithPods("paca", envPodFixture("paca-env-envE-pending", backendRef, corev1.PodPending, ""))

	if _, err := m.podNameForEnvironment(context.Background(), backendRef); err == nil {
		t.Error("podNameForEnvironment: expected an error when the only matching pod isn't Running, got nil")
	}
}

func TestWaitForPodIP_ResolvesEnvironmentSelector(t *testing.T) {
	// waitForPodIP itself is generic (see its own doc comment in
	// manager.go) — this confirms it works with environmentLabel's
	// selector shape too, not just a Job's job-name one.
	const backendRef = "paca-env-envB"
	m := managerWithPods("paca", envPodFixture("paca-env-envB-x1", backendRef, corev1.PodRunning, "10.0.0.7"))

	podName, podIP, err := m.waitForPodIP(context.Background(), environmentLabel+"="+backendRef)
	if err != nil {
		t.Fatalf("waitForPodIP: %v", err)
	}
	if podName != "paca-env-envB-x1" || podIP != "10.0.0.7" {
		t.Errorf("waitForPodIP = (%q, %q), want (%q, %q)", podName, podIP, "paca-env-envB-x1", "10.0.0.7")
	}
}

func managerWithDeployment(namespace string, dep *appsv1.Deployment) *Manager {
	clientset := fake.NewClientset()
	if _, err := clientset.AppsV1().Deployments(namespace).Create(context.Background(), dep, metav1.CreateOptions{}); err != nil {
		panic(err)
	}
	return &Manager{clientset: clientset, namespace: namespace}
}

func deploymentFixture(name string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{environmentLabel: name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{environmentLabel: name}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: containerName, Image: "img"}}},
			},
		},
	}
}

func TestScaleDeployment_SetsReplicas(t *testing.T) {
	const name = "paca-env-scale1"
	m := managerWithDeployment("paca", deploymentFixture(name, 0))

	if err := m.scaleDeployment(context.Background(), name, 1); err != nil {
		t.Fatalf("scaleDeployment: %v", err)
	}

	got, err := m.clientset.AppsV1().Deployments("paca").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 1 {
		t.Errorf("deployment replicas after scaleDeployment = %v, want 1", got.Spec.Replicas)
	}
}

// TestScaleDeployment_IsIdempotentWhenAlreadyAtTargetReplicas covers the
// StartEnvironment-called-twice case its own doc comment describes as
// safe: patching replicas to a value it's already at must not error and
// must leave the Deployment's Pod template (the only thing that could
// trigger a Pod restart) untouched.
func TestScaleDeployment_IsIdempotentWhenAlreadyAtTargetReplicas(t *testing.T) {
	const name = "paca-env-scale2"
	m := managerWithDeployment("paca", deploymentFixture(name, 1))

	beforeTemplate := deploymentFixture(name, 1).Spec.Template

	if err := m.scaleDeployment(context.Background(), name, 1); err != nil {
		t.Fatalf("first scaleDeployment(1): %v", err)
	}
	if err := m.scaleDeployment(context.Background(), name, 1); err != nil {
		t.Fatalf("second scaleDeployment(1): %v", err)
	}

	got, err := m.clientset.AppsV1().Deployments("paca").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 1 {
		t.Errorf("deployment replicas after repeated scaleDeployment(1) = %v, want 1", got.Spec.Replicas)
	}
	if len(got.Spec.Template.Spec.Containers) != len(beforeTemplate.Spec.Containers) ||
		got.Spec.Template.Spec.Containers[0].Image != beforeTemplate.Spec.Containers[0].Image {
		t.Errorf("scaleDeployment must not touch spec.template — got %+v, want unchanged %+v",
			got.Spec.Template.Spec.Containers, beforeTemplate.Spec.Containers)
	}
}

func TestScaleDeployment_ErrorsWhenDeploymentMissing(t *testing.T) {
	m := managerWithPods("paca") // fake clientset, no deployments created

	if err := m.scaleDeployment(context.Background(), "paca-env-missing", 1); err == nil {
		t.Error("scaleDeployment: expected an error for a nonexistent deployment, got nil")
	}
}

func TestDeleteEnvironment_DeletesDeploymentAndPVC(t *testing.T) {
	const name = "paca-env-del1"
	clientset := fake.NewClientset()
	m := &Manager{clientset: clientset, namespace: "paca"}
	if _, err := clientset.AppsV1().Deployments("paca").Create(context.Background(), deploymentFixture(name, 1), metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	pvc := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if _, err := clientset.CoreV1().PersistentVolumeClaims("paca").Create(context.Background(), pvc, metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed pvc: %v", err)
	}

	if err := m.DeleteEnvironment(context.Background(), name, name); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}

	if _, err := clientset.AppsV1().Deployments("paca").Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Error("DeleteEnvironment: deployment still exists after delete")
	}
	if _, err := clientset.CoreV1().PersistentVolumeClaims("paca").Get(context.Background(), name, metav1.GetOptions{}); err == nil {
		t.Error("DeleteEnvironment: pvc still exists after delete")
	}
}

// TestDeleteEnvironment_IsIdempotentOnAlreadyDeletedObjects mirrors
// deleteJob's own "already-gone is not an error" stance (manager.go) — a
// caller retrying DeleteEnvironment after a partial failure must not get
// a spurious NotFound error for whichever object it already removed.
func TestDeleteEnvironment_IsIdempotentOnAlreadyDeletedObjects(t *testing.T) {
	m := managerWithPods("paca") // nothing seeded at all

	if err := m.DeleteEnvironment(context.Background(), "paca-env-nope", "paca-env-nope"); err != nil {
		t.Errorf("DeleteEnvironment on already-gone objects = %v, want nil", err)
	}
}

func TestDiagnoseUnreadyEnvironment_ReportsPodNameAndPhase(t *testing.T) {
	const backendRef = "paca-env-envC"
	m := managerWithPods("paca", envPodFixture("paca-env-envC-zz", backendRef, corev1.PodPending, ""))

	got := m.diagnoseUnreadyEnvironment(context.Background(), backendRef)
	if !strings.Contains(got, "paca-env-envC-zz") {
		t.Errorf("diagnoseUnreadyEnvironment = %q, want it to mention the pod name", got)
	}
	if !strings.Contains(got, "Pending") {
		t.Errorf("diagnoseUnreadyEnvironment = %q, want it to mention the pod phase", got)
	}
}

func TestDiagnoseUnreadyEnvironment_ReportsWhenNoPodFound(t *testing.T) {
	m := managerWithPods("paca")

	got := m.diagnoseUnreadyEnvironment(context.Background(), "paca-env-missing")
	if !strings.Contains(got, "no pod found") {
		t.Errorf("diagnoseUnreadyEnvironment = %q, want it to say no pod was found", got)
	}
}

func TestResizeQueue_TranslatesSize(t *testing.T) {
	ch := make(chan sandbox.TermSize, 1)
	ch <- sandbox.TermSize{Rows: 24, Cols: 80}
	q := &resizeQueue{ctx: context.Background(), resize: ch}

	got := q.Next()
	if got == nil {
		t.Fatal("resizeQueue.Next() = nil, want a TerminalSize")
	}
	if got.Width != 80 || got.Height != 24 {
		t.Errorf("resizeQueue.Next() = %+v, want Width=80 Height=24", got)
	}
}

func TestResizeQueue_ReturnsNilWhenChannelCloses(t *testing.T) {
	ch := make(chan sandbox.TermSize)
	close(ch)
	q := &resizeQueue{ctx: context.Background(), resize: ch}

	if got := q.Next(); got != nil {
		t.Errorf("resizeQueue.Next() on a closed channel = %+v, want nil", got)
	}
}

func TestResizeQueue_ReturnsNilWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan sandbox.TermSize)
	q := &resizeQueue{ctx: ctx, resize: ch}

	done := make(chan *remotecommand.TerminalSize, 1)
	go func() {
		done <- q.Next()
	}()

	select {
	case got := <-done:
		if got != nil {
			t.Errorf("resizeQueue.Next() with a cancelled context = %+v, want nil", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resizeQueue.Next() did not return promptly when ctx was already cancelled")
	}
}

func TestEnvironmentServicePortName_SSHGetsAFixedName(t *testing.T) {
	if got := environmentServicePortName(sandbox.EnvironmentSSHPort); got != "ssh" {
		t.Errorf("environmentServicePortName(%d) = %q, want \"ssh\"", sandbox.EnvironmentSSHPort, got)
	}
}

func TestEnvironmentServicePortName_ForwardsAreNamedByContainerPort(t *testing.T) {
	if got := environmentServicePortName(3000); got != "pf-3000" {
		t.Errorf("environmentServicePortName(3000) = %q, want \"pf-3000\"", got)
	}
}

func TestEnvironmentServicePorts_BuildsOneEntryPerMapping(t *testing.T) {
	ports := environmentServicePorts([]sandbox.PortMapping{
		{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: 22001},
		{ContainerPort: 3000, HostPort: 30001},
	})

	if len(ports) != 2 {
		t.Fatalf("environmentServicePorts returned %d entries, want 2", len(ports))
	}
	if ports[0].Name != "ssh" || ports[0].NodePort != 22001 || ports[0].Port != int32(sandbox.EnvironmentSSHPort) {
		t.Errorf("ports[0] = %+v, want ssh entry on 22001", ports[0])
	}
	if ports[1].Name != "pf-3000" || ports[1].NodePort != 30001 || ports[1].Port != 3000 {
		t.Errorf("ports[1] = %+v, want pf-3000 entry on 30001", ports[1])
	}
}

func managerWithNamespace(namespace string) *Manager {
	return &Manager{clientset: fake.NewClientset(), namespace: namespace}
}

// TestEnsureEnvironmentService_CreatesWhenMissing verifies a brand-new
// environment (CreateEnvironment's own call site) gets a NodePort Service
// with the given mappings when none exists yet.
func TestEnsureEnvironmentService_CreatesWhenMissing(t *testing.T) {
	const name = "paca-env-svc1"
	m := managerWithNamespace("paca")
	labels := map[string]string{environmentLabel: name}

	err := m.ensureEnvironmentService(context.Background(), name, labels, []sandbox.PortMapping{
		{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: 22001},
	})
	if err != nil {
		t.Fatalf("ensureEnvironmentService: %v", err)
	}

	svc, err := m.clientset.CoreV1().Services("paca").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if svc.Spec.Type != corev1.ServiceTypeNodePort {
		t.Errorf("service type = %v, want NodePort", svc.Spec.Type)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].NodePort != 22001 {
		t.Errorf("service ports = %+v, want one entry with NodePort 22001", svc.Spec.Ports)
	}
}

// TestEnsureEnvironmentService_PatchesPortsWhenExists verifies calling it
// again with a different mapping set (the RestartEnvironmentPorts case —
// a user added a forward) replaces spec.ports wholesale rather than
// erroring on an already-existing Service.
func TestEnsureEnvironmentService_PatchesPortsWhenExists(t *testing.T) {
	const name = "paca-env-svc2"
	m := managerWithNamespace("paca")
	labels := map[string]string{environmentLabel: name}
	ctx := context.Background()

	if err := m.ensureEnvironmentService(ctx, name, labels, []sandbox.PortMapping{
		{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: 22001},
	}); err != nil {
		t.Fatalf("initial ensureEnvironmentService: %v", err)
	}

	if err := m.ensureEnvironmentService(ctx, name, labels, []sandbox.PortMapping{
		{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: 22001},
		{ContainerPort: 3000, HostPort: 30001},
	}); err != nil {
		t.Fatalf("second ensureEnvironmentService: %v", err)
	}

	svc, err := m.clientset.CoreV1().Services("paca").Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("service ports after patch = %+v, want 2 entries", svc.Spec.Ports)
	}
}

// TestEnsureEnvironmentService_DeletesWhenMappingsEmpty verifies the
// "user deleted their only forward and SSH isn't configured" case: an
// existing Service is removed rather than left with an invalid empty
// spec.ports.
func TestEnsureEnvironmentService_DeletesWhenMappingsEmpty(t *testing.T) {
	const name = "paca-env-svc3"
	m := managerWithNamespace("paca")
	labels := map[string]string{environmentLabel: name}
	ctx := context.Background()

	if err := m.ensureEnvironmentService(ctx, name, labels, []sandbox.PortMapping{
		{ContainerPort: sandbox.EnvironmentSSHPort, HostPort: 22001},
	}); err != nil {
		t.Fatalf("initial ensureEnvironmentService: %v", err)
	}

	if err := m.ensureEnvironmentService(ctx, name, labels, nil); err != nil {
		t.Fatalf("ensureEnvironmentService with empty mappings: %v", err)
	}

	if _, err := m.clientset.CoreV1().Services("paca").Get(ctx, name, metav1.GetOptions{}); err == nil {
		t.Error("service still exists after ensureEnvironmentService with empty mappings, want it deleted")
	}
}

// TestEnsureEnvironmentService_NoopWhenAlreadyAbsentAndMappingsEmpty
// verifies DeleteEnvironment's own call site (an environment that never
// had any ports configured at all) doesn't error just because there was
// never a Service to delete.
func TestEnsureEnvironmentService_NoopWhenAlreadyAbsentAndMappingsEmpty(t *testing.T) {
	m := managerWithNamespace("paca")

	if err := m.ensureEnvironmentService(context.Background(), "paca-env-svc4", nil, nil); err != nil {
		t.Errorf("ensureEnvironmentService on a never-created service = %v, want nil error", err)
	}
}

// deploymentFixtureWithEnv mirrors deploymentFixture but lets a test set
// the container's own starting Env — what ensureEnvironmentEnv reads to
// decide whether GOOSE_PROVIDER still needs backfilling.
func deploymentFixtureWithEnv(name string, env []corev1.EnvVar) *appsv1.Deployment {
	dep := deploymentFixture(name, 1)
	dep.Spec.Template.Spec.Containers[0].Env = env
	return dep
}

// TestEnsureEnvironmentEnv_BackfillsMissingProvider covers the actual bug
// this method fixes: a Deployment created by handleCreateEnvironment (no
// agent/LLM context) never got GOOSE_PROVIDER, so goose serve fails every
// session/new until the first conversation attach backfills it here.
func TestEnsureEnvironmentEnv_BackfillsMissingProvider(t *testing.T) {
	const name = "paca-env-env1"
	m := managerWithDeployment("paca", deploymentFixtureWithEnv(name, nil))

	cfg := sandbox.EnvironmentConfig{Env: map[string]string{
		"GOOSE_PROVIDER":    "anthropic",
		"GOOSE_MODEL":       "claude-sonnet-5",
		"ANTHROPIC_API_KEY": "sk-test",
	}}
	if err := m.ensureEnvironmentEnv(context.Background(), name, cfg); err != nil {
		t.Fatalf("ensureEnvironmentEnv: %v", err)
	}

	got, err := m.clientset.AppsV1().Deployments("paca").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	gotEnv := map[string]string{}
	for _, ev := range got.Spec.Template.Spec.Containers[0].Env {
		gotEnv[ev.Name] = ev.Value
	}
	for k, want := range cfg.Env {
		if gotEnv[k] != want {
			t.Errorf("deployment env[%q] = %q, want %q", k, gotEnv[k], want)
		}
	}
}

// TestEnsureEnvironmentEnv_NoopWhenProviderAlreadyPresent verifies the
// "fixed for the environment's whole life" semantic: once GOOSE_PROVIDER
// is set, a later conversation attach from a different agent (a different
// provider in cfg.Env) must not overwrite it — the existing value wins,
// unchanged.
func TestEnsureEnvironmentEnv_NoopWhenProviderAlreadyPresent(t *testing.T) {
	const name = "paca-env-env2"
	existing := []corev1.EnvVar{{Name: "GOOSE_PROVIDER", Value: "anthropic"}}
	m := managerWithDeployment("paca", deploymentFixtureWithEnv(name, existing))

	cfg := sandbox.EnvironmentConfig{Env: map[string]string{"GOOSE_PROVIDER": "openai"}}
	if err := m.ensureEnvironmentEnv(context.Background(), name, cfg); err != nil {
		t.Fatalf("ensureEnvironmentEnv: %v", err)
	}

	got, err := m.clientset.AppsV1().Deployments("paca").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	gotEnv := got.Spec.Template.Spec.Containers[0].Env
	if len(gotEnv) != 1 || gotEnv[0].Name != "GOOSE_PROVIDER" || gotEnv[0].Value != "anthropic" {
		t.Errorf("deployment env after ensureEnvironmentEnv = %+v, want unchanged [GOOSE_PROVIDER=anthropic]", gotEnv)
	}
}

// TestEnsureEnvironmentEnv_NoopWhenCfgHasNoProvider covers the ephemeral/
// unconfigured case (e.g. a caller that never set up LLM env at all) —
// nothing to backfill, so the Deployment must be left untouched.
func TestEnsureEnvironmentEnv_NoopWhenCfgHasNoProvider(t *testing.T) {
	const name = "paca-env-env3"
	m := managerWithDeployment("paca", deploymentFixtureWithEnv(name, nil))

	if err := m.ensureEnvironmentEnv(context.Background(), name, sandbox.EnvironmentConfig{}); err != nil {
		t.Fatalf("ensureEnvironmentEnv: %v", err)
	}

	got, err := m.clientset.AppsV1().Deployments("paca").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment: %v", err)
	}
	if len(got.Spec.Template.Spec.Containers[0].Env) != 0 {
		t.Errorf("deployment env after ensureEnvironmentEnv with no cfg.Env = %+v, want empty", got.Spec.Template.Spec.Containers[0].Env)
	}
}
