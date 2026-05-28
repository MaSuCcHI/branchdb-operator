package domain

import (
	"context"
	"fmt"
)

// BranchDatabaseProvider はブランチデータベースの起動/停止の抽象インターフェース。
// MySQL・PostgreSQL・Redis など複数のデータベースエンジンに対応する。
type BranchDatabaseProvider interface {
	// Start はブランチ用データベースを起動し接続情報を返す。
	// dbType: "mysql" / "postgres" / "redis"（空文字列はデフォルト "mysql" として扱う）
	// dbVersion: イメージタグ上書き（空文字列はデフォルトバージョンを使用）
	Start(ctx context.Context, branch string, vol VolumeInfo, dbType, dbVersion string) (BranchEndpoint, error)
	Stop(ctx context.Context, branch string) error
}

// BranchMySQLProvider は後方互換性のためのエイリアス。
// 新しいコードは BranchDatabaseProvider を使用すること。
//
// Deprecated: Use BranchDatabaseProvider.
type BranchMySQLProvider = BranchDatabaseProvider

// BranchEndpoint はブランチデータベースへの接続情報を保持する値型。
type BranchEndpoint struct {
	Host         string // ClusterIP Service の DNS 名
	Port         int    // デフォルトポート（MySQL=3306, PostgreSQL=5432, Redis=6379）
	ExternalPort int    // NodePort（K8s モードのみ。0 = 未割当）
}

// DSN はユーザーとパスワードを含む DSN 文字列を返す。MySQL 形式。
func (e BranchEndpoint) DSN(user, pass string) string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/", user, pass, e.Host, e.Port)
}
