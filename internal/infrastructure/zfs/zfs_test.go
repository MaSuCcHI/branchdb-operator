package zfs

import (
	"testing"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
)

// parseSnapshotList のテスト（純粋関数: exec に依存しない）
// zfs list -p で epoch 秒形式の出力を使う
func TestParseSnapshotList(t *testing.T) {
	dataset := "tank/mysql"
	input := []byte(
		"tank/mysql@base\t1735689600\n" +
			"tank/mysql@feature-login\t1735776000\n" +
			"\n", // 空行は無視される
	)
	got, err := parseSnapshotList(input, dataset)
	if err != nil {
		t.Fatalf("parseSnapshotList: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "base" {
		t.Errorf("got[0].Name = %q, want %q", got[0].Name, "base")
	}
	if got[1].Name != "feature-login" {
		t.Errorf("got[1].Name = %q, want %q", got[1].Name, "feature-login")
	}
}

func TestParseSnapshotList_Empty(t *testing.T) {
	got, err := parseSnapshotList([]byte(""), "tank/mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(got))
	}
}

func TestParseSnapshotList_SkipsShortLines(t *testing.T) {
	input := []byte("tank/mysql@only-one-field\n")
	got, err := parseSnapshotList(input, "tank/mysql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// フィールドが1つだけの行はスキップされる
	if len(got) != 0 {
		t.Errorf("expected 0 entries for short line, got %d", len(got))
	}
}

func TestParseSnapshotList_EpochSeconds(t *testing.T) {
	dataset := "tank/mysql"
	// zfs list -p -H -o name,creation の出力形式: <name>\t<epoch秒>
	input := []byte(
		"tank/mysql@base\t1735689600\n" + // 2025-01-01 00:00:00 UTC
			"tank/mysql@feature-login\t1735776000\n", // 2025-01-02 00:00:00 UTC
	)
	got, err := parseSnapshotList(input, dataset)
	if err != nil {
		t.Fatalf("parseSnapshotList: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].CreatedAt.IsZero() {
		t.Error("got[0].CreatedAt is zero, epoch parsing must have failed")
	}
	wantTime := time.Unix(1735689600, 0).UTC()
	if !got[0].CreatedAt.Equal(wantTime) {
		t.Errorf("got[0].CreatedAt = %v, want %v", got[0].CreatedAt, wantTime)
	}
}

// isAllDigits のテスト
func TestIsAllDigits(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"12345678", true},
		{"123456", true},
		{"", false},
		{"1234567a", false},
		{"abcdefgh", false},
		{"1234 567", false},
	}
	for _, tt := range tests {
		got := isAllDigits(tt.s)
		if got != tt.want {
			t.Errorf("isAllDigits(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

// sortSnapshotsByCreatedAtDesc のテスト
func TestSortSnapshotsByCreatedAtDesc(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)

	snaps := []domain.SnapshotInfo{
		{Name: "old", CreatedAt: t1},
		{Name: "new", CreatedAt: t3},
		{Name: "mid", CreatedAt: t2},
	}
	sortSnapshotsByCreatedAtDesc(snaps)

	if snaps[0].Name != "new" {
		t.Errorf("snaps[0] = %q, want %q", snaps[0].Name, "new")
	}
	if snaps[1].Name != "mid" {
		t.Errorf("snaps[1] = %q, want %q", snaps[1].Name, "mid")
	}
	if snaps[2].Name != "old" {
		t.Errorf("snaps[2] = %q, want %q", snaps[2].Name, "old")
	}
}

func TestSortSnapshotsByCreatedAtDesc_Single(t *testing.T) {
	snaps := []domain.SnapshotInfo{{Name: "only"}}
	sortSnapshotsByCreatedAtDesc(snaps)
	if snaps[0].Name != "only" {
		t.Errorf("unexpected: %q", snaps[0].Name)
	}
}

// NewAgentProvider / clonePath のテスト
func TestNewAgentProvider(t *testing.T) {
	p := NewAgentProvider("tank/mysql")
	if p == nil {
		t.Fatal("NewAgentProvider returned nil")
	}
	if p.dataset != "tank/mysql" {
		t.Errorf("dataset = %q, want %q", p.dataset, "tank/mysql")
	}
}

func TestAgentProvider_ClonePath(t *testing.T) {
	p := NewAgentProvider("tank/mysql")
	got := p.clonePath("feature-login")
	want := "tank/mysql/branches/feature-login"
	if got != want {
		t.Errorf("clonePath() = %q, want %q", got, want)
	}
}

// NewClient のテスト
func TestNewClient(t *testing.T) {
	c := NewClient("tank/postgres")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.dataset != "tank/postgres" {
		t.Errorf("dataset = %q, want %q", c.dataset, "tank/postgres")
	}
}
