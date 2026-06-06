package domain_test

import (
	"testing"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
)

func TestBranchEndpoint_DSN(t *testing.T) {
	tests := []struct {
		name string
		ep   domain.BranchEndpoint
		user string
		pass string
		want string
	}{
		{
			name: "basic",
			ep:   domain.BranchEndpoint{Host: "svc.default.svc.cluster.local", Port: 3306},
			user: "root",
			pass: "secret",
			want: "root:secret@tcp(svc.default.svc.cluster.local:3306)/",
		},
		{
			name: "empty password",
			ep:   domain.BranchEndpoint{Host: "localhost", Port: 3306},
			user: "admin",
			pass: "",
			want: "admin:@tcp(localhost:3306)/",
		},
		{
			name: "postgres port",
			ep:   domain.BranchEndpoint{Host: "db", Port: 5432, ExternalPort: 30000},
			user: "pg",
			pass: "pw",
			want: "pg:pw@tcp(db:5432)/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.ep.DSN(tt.user, tt.pass)
			if got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVolumeInfo_Fields(t *testing.T) {
	vi := domain.VolumeInfo{
		CloneName: "feature-login",
		NFSServer: "10.0.0.5",
		NFSPath:   "/tank/mysql/branches/feature-login",
	}
	if vi.CloneName != "feature-login" {
		t.Errorf("CloneName = %q", vi.CloneName)
	}
	if vi.NFSServer != "10.0.0.5" {
		t.Errorf("NFSServer = %q", vi.NFSServer)
	}
	if vi.NFSPath != "/tank/mysql/branches/feature-login" {
		t.Errorf("NFSPath = %q", vi.NFSPath)
	}
}

func TestSnapshotInfo_Fields(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	si := domain.SnapshotInfo{
		Name:         "base",
		CreatedAt:    now,
		DatabaseType: "mysql",
	}
	if si.Name != "base" {
		t.Errorf("Name = %q", si.Name)
	}
	if !si.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt mismatch")
	}
	if si.DatabaseType != "mysql" {
		t.Errorf("DatabaseType = %q", si.DatabaseType)
	}
}

func TestGCReport_Fields(t *testing.T) {
	r := domain.GCReport{
		DeletedOrphanClones: []string{"clone-a", "clone-b"},
		DeletedSnapshots:    []string{"snap-1"},
	}
	if len(r.DeletedOrphanClones) != 2 {
		t.Errorf("DeletedOrphanClones len = %d", len(r.DeletedOrphanClones))
	}
	if len(r.DeletedSnapshots) != 1 {
		t.Errorf("DeletedSnapshots len = %d", len(r.DeletedSnapshots))
	}
}

func TestBranchEndpoint_ExternalPort(t *testing.T) {
	ep := domain.BranchEndpoint{Host: "svc", Port: 3306, ExternalPort: 30001}
	if ep.ExternalPort != 30001 {
		t.Errorf("ExternalPort = %d", ep.ExternalPort)
	}
}
