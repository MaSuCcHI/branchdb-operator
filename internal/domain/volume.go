package domain

import (
	"context"
	"time"
)

// VolumeProvider は ZFS クローン/スナップショット操作の抽象インターフェース。
// 実装を切り替えることで ZFS Server と AWS FSx に対応する。
type VolumeProvider interface {
	TakeSnapshot(ctx context.Context, name string) error
	CreateClone(ctx context.Context, snapshot, cloneName string) (VolumeInfo, error)
	DeleteClone(ctx context.Context, cloneName string) error
	ListSnapshots(ctx context.Context) ([]SnapshotInfo, error)
}

// VolumeInfo はクローンボリュームの接続情報を保持する値型。
type VolumeInfo struct {
	CloneName string
	NFSServer string // "10.0.0.5" or FSx DNS
	NFSPath   string // "/tank/branches/feature-login"
}

// SnapshotInfo はスナップショットのメタデータを保持する値型。
type SnapshotInfo struct {
	Name      string
	CreatedAt time.Time
}
