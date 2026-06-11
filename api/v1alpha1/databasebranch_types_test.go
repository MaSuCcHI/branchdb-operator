package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDatabaseBranch_DeepCopy(t *testing.T) {
	expiresAt := metav1.NewTime(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))

	original := &DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-branch",
			Namespace: "default",
		},
		Spec: DatabaseBranchSpec{
			SnapshotRef: "snapshot-001",
			TTLHours:    24,
		},
		Status: DatabaseBranchStatus{
			Phase:        BranchPhaseReady,
			ClusterHost:  "mysql.cluster.local",
			ClusterPort:  3306,
			ExternalHost: "branch.example.com",
			ExternalPort: 30306,
			Message:      "Branch is ready",
			ExpiresAt:    &expiresAt,
		},
	}

	copied := original.DeepCopy()

	// コピーが元と等しいこと
	if copied.Name != original.Name {
		t.Errorf("DeepCopy: Name = %q, want %q", copied.Name, original.Name)
	}
	if copied.Spec.SnapshotRef != original.Spec.SnapshotRef {
		t.Errorf("DeepCopy: Spec.SnapshotRef = %q, want %q", copied.Spec.SnapshotRef, original.Spec.SnapshotRef)
	}
	if copied.Spec.TTLHours != original.Spec.TTLHours {
		t.Errorf("DeepCopy: Spec.TTLHours = %d, want %d", copied.Spec.TTLHours, original.Spec.TTLHours)
	}
	if copied.Status.Phase != original.Status.Phase {
		t.Errorf("DeepCopy: Status.Phase = %q, want %q", copied.Status.Phase, original.Status.Phase)
	}
	if copied.Status.ExternalPort != original.Status.ExternalPort {
		t.Errorf("DeepCopy: Status.ExternalPort = %d, want %d", copied.Status.ExternalPort, original.Status.ExternalPort)
	}
	if copied.Status.ExpiresAt == nil {
		t.Fatal("DeepCopy: Status.ExpiresAt が nil になっている")
	}
	if !copied.Status.ExpiresAt.Equal(original.Status.ExpiresAt) {
		t.Errorf("DeepCopy: Status.ExpiresAt = %v, want %v", copied.Status.ExpiresAt, original.Status.ExpiresAt)
	}

	// ディープコピーであること（ポインタが独立していること）
	copied.Status.ExpiresAt.Time = time.Now()
	if original.Status.ExpiresAt.Equal(copied.Status.ExpiresAt) {
		t.Error("DeepCopy: ExpiresAt がシャローコピーになっている（独立していない）")
	}
}

func TestDatabaseBranchList_DeepCopy(t *testing.T) {
	list := &DatabaseBranchList{
		Items: []DatabaseBranch{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "branch-1"},
				Spec:       DatabaseBranchSpec{SnapshotRef: "snap-1"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "branch-2"},
				Spec:       DatabaseBranchSpec{SnapshotRef: "snap-2"},
			},
		},
	}

	copied := list.DeepCopy()

	if len(copied.Items) != len(list.Items) {
		t.Fatalf("DeepCopy: Items の長さ = %d, want %d", len(copied.Items), len(list.Items))
	}
	if copied.Items[0].Name != list.Items[0].Name {
		t.Errorf("DeepCopy: Items[0].Name = %q, want %q", copied.Items[0].Name, list.Items[0].Name)
	}
}

func TestDatabaseBranch_DeepCopy_nilレシーバーはnilを返す(t *testing.T) {
	var b *DatabaseBranch
	if b.DeepCopy() != nil {
		t.Error("nil レシーバーの DeepCopy は nil を返すべき")
	}

	var list *DatabaseBranchList
	if list.DeepCopy() != nil {
		t.Error("nil レシーバーの DeepCopy は nil を返すべき")
	}
}

func TestDatabaseBranch_DeepCopyObject_runtime_Objectインターフェースを満たす(t *testing.T) {
	b := &DatabaseBranch{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}
	obj := b.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject が nil を返した")
	}
	copied, ok := obj.(*DatabaseBranch)
	if !ok {
		t.Fatalf("DeepCopyObject の返り値が *DatabaseBranch でない: %T", obj)
	}
	if copied.Name != b.Name {
		t.Errorf("DeepCopyObject: Name = %q, want %q", copied.Name, b.Name)
	}

	list := &DatabaseBranchList{
		Items: []DatabaseBranch{{ObjectMeta: metav1.ObjectMeta{Name: "item-1"}}},
	}
	listObj := list.DeepCopyObject()
	if listObj == nil {
		t.Fatal("DatabaseBranchList.DeepCopyObject が nil を返した")
	}
}

func TestBranchPhase_定数値(t *testing.T) {
	tests := []struct {
		phase BranchPhase
		want  string
	}{
		{BranchPhasePending, "Pending"},
		{BranchPhaseCreating, "Creating"},
		{BranchPhaseReady, "Ready"},
		{BranchPhaseError, "Error"},
		{BranchPhaseDeleting, "Deleting"},
	}

	for _, tt := range tests {
		if string(tt.phase) != tt.want {
			t.Errorf("BranchPhase %q: 期待値 %q", tt.phase, tt.want)
		}
	}
}
