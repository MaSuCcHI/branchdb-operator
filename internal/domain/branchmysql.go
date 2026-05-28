package domain

import (
	"context"
	"fmt"
)

// BranchMySQLProvider はブランチ MySQL の起動/停止の抽象インターフェース。
type BranchMySQLProvider interface {
	Start(ctx context.Context, branch string, vol VolumeInfo) (BranchEndpoint, error)
	Stop(ctx context.Context, branch string) error
}

// BranchEndpoint はブランチ MySQL への接続情報を保持する値型。
type BranchEndpoint struct {
	Host         string // ClusterIP Service の DNS 名
	Port         int    // 3306
	ExternalPort int    // NodePort（K8s モードのみ。0 = 未割当）
}

// DSN はユーザーとパスワードを含む MySQL DSN 文字列を返す。
func (e BranchEndpoint) DSN(user, pass string) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/", user, pass, e.Host, e.Port)
}
