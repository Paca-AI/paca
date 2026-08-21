package k8s

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func authoritativeJob(name string, turnID, runID uuid.UUID) *batchv1.Job {
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: name, Namespace: "paca",
		Labels: map[string]string{
			labelManaged: managedValue,
			labelTurnID:  turnID.String(),
			labelConvID:  runID.String(),
		},
	}}
}

func TestReapAuthoritativeOrphansKeepsActiveAndDeletesInactiveJobs(t *testing.T) {
	const namespace = "paca"
	activeTurn, activeRun := uuid.New(), uuid.New()
	orphanTurn, orphanRun := uuid.New(), uuid.New()
	legacy := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{
		Name: "legacy", Namespace: namespace,
		Labels: map[string]string{labelManaged: managedValue, labelConvID: uuid.NewString()},
	}}
	clientset := fake.NewClientset(
		authoritativeJob("active", activeTurn, activeRun),
		authoritativeJob("orphan", orphanTurn, orphanRun),
		legacy,
	)
	m := &Manager{clientset: clientset, namespace: namespace}

	reaped, err := m.ReapAuthoritativeOrphans(context.Background(), func(_ context.Context, turnID, runID uuid.UUID) (bool, error) {
		return turnID == activeTurn && runID == activeRun, nil
	})
	if err != nil {
		t.Fatalf("ReapAuthoritativeOrphans: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("reaped = %d, want 1", reaped)
	}
	for _, name := range []string{"active", "legacy"} {
		if _, err := clientset.BatchV1().Jobs(namespace).Get(context.Background(), name, metav1.GetOptions{}); err != nil {
			t.Errorf("job %s should remain: %v", name, err)
		}
	}
	if _, err := clientset.BatchV1().Jobs(namespace).Get(context.Background(), "orphan", metav1.GetOptions{}); err == nil {
		t.Error("orphan job still exists after reaping")
	}
}

func TestReapAuthoritativeOrphansFailsSafeOnControlPlaneError(t *testing.T) {
	const namespace = "paca"
	turnID, runID := uuid.New(), uuid.New()
	clientset := fake.NewClientset(authoritativeJob("uncertain", turnID, runID))
	m := &Manager{clientset: clientset, namespace: namespace}
	wantErr := errors.New("control plane unavailable")

	reaped, err := m.ReapAuthoritativeOrphans(context.Background(), func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
		return false, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
	if reaped != 0 {
		t.Fatalf("reaped = %d, want 0", reaped)
	}
	if _, err := clientset.BatchV1().Jobs(namespace).Get(context.Background(), "uncertain", metav1.GetOptions{}); err != nil {
		t.Fatalf("job should remain when runtime state is uncertain: %v", err)
	}
}
