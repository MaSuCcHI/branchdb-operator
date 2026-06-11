package zfs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
	"github.com/MaSuCcHI/branchdb-operator/internal/interface/api/zfsagent"
)

// AgentProvider は AgentVolumeProvider インターフェースを実装し、
// ZFS コマンドを直接呼び出してスナップショット・クローン操作を行う。
// このコードは os/exec に依存するため、ユニットテストの 95% カバレッジ対象外。
type AgentProvider struct {
	// dataset は "tank/mysql" のように pool/dataset 形式で指定する。
	dataset string
}

// NewAgentProvider は AgentProvider を生成する。
func NewAgentProvider(dataset string) *AgentProvider {
	return &AgentProvider{dataset: dataset}
}

// Ensure AgentProvider implements AgentVolumeProvider at compile time.
var _ zfsagent.AgentVolumeProvider = (*AgentProvider)(nil)

// TakeSnapshot はスナップショットを作成する。
// overwrite が true の場合:
//   - 依存クローンがなければ既存スナップショットを削除して新規作成。
//   - 依存クローン（ブランチ）がある場合は既存スナップショットをリネームして
//     アーカイブし（例: base → base-20260531-150405）、同名の新スナップショットを作成する。
//     ZFS は rename 時にクローンの origin を自動更新するため、既存ブランチは引き続き動作する。
func (p *AgentProvider) TakeSnapshot(ctx context.Context, name string, overwrite bool) error {
	zfsName := fmt.Sprintf("%s@%s", p.dataset, name)
	if overwrite {
		out, err := exec.CommandContext(ctx, "zfs", "destroy", zfsName).CombinedOutput()
		if err != nil {
			outStr := strings.TrimSpace(string(out))
			switch {
			case strings.Contains(outStr, "dataset does not exist"):
				// スナップショットが存在しない → そのまま新規作成
			case strings.Contains(outStr, "has dependent clones"):
				// 既存スナップショットをタイムスタンプ付きでアーカイブ
				archiveName := fmt.Sprintf("%s@%s-%s", p.dataset, name, time.Now().Format("20060102-150405"))
				if renameErr := run(ctx, "zfs", "rename", zfsName, archiveName); renameErr != nil {
					return fmt.Errorf("zfs rename snapshot: %w", renameErr)
				}
				// fall through: 同名の新スナップショットを作成
			default:
				return fmt.Errorf("zfs destroy snapshot: %w (output: %s)", err, outStr)
			}
		}
	}
	return run(ctx, "zfs", "snapshot", zfsName)
}

// DeleteSnapshot はスナップショットを削除する。
func (p *AgentProvider) DeleteSnapshot(ctx context.Context, name string) error {
	zfsName := fmt.Sprintf("%s@%s", p.dataset, name)
	out, err := exec.CommandContext(ctx, "zfs", "destroy", zfsName).CombinedOutput()
	if err != nil {
		outStr := strings.TrimSpace(string(out))
		switch {
		case strings.Contains(outStr, "dataset does not exist"):
			return zfsagent.ErrNotFound
		case strings.Contains(outStr, "has dependent clones"):
			return fmt.Errorf("snapshot %q has dependent clones (delete all branches using this snapshot first)", name)
		default:
			return fmt.Errorf("zfs destroy snapshot: %w (output: %s)", err, outStr)
		}
	}
	return nil
}

// ListSnapshots はスナップショット一覧を返す。
// `-p` フラグで epoch 秒として creation を取得することで、ロケール依存を回避する。
func (p *AgentProvider) ListSnapshots(ctx context.Context) ([]domain.SnapshotInfo, error) {
	out, err := exec.CommandContext(ctx, "zfs", "list", "-t", "snapshot", "-p", "-H", "-o", "name,creation", p.dataset).Output()
	if err != nil {
		return nil, fmt.Errorf("zfs list snapshots: %w", err)
	}
	return parseSnapshotList(out, p.dataset)
}

// CreateClone は指定スナップショットからクローンを作成し、接続情報を返す。
//
// クローン作成後に必ず `zfs share <target>` を呼び出し、NFS エクスポートを確立する。
// 親 dataset に sharenfs プロパティが設定されていない場合は share が失敗するため、
// ZFS サーバー側で以下のように設定しておく必要がある:
//
//	zfs set sharenfs="rw=@<k8s-cidr>,no_root_squash" <pool>/<dataset>
func (p *AgentProvider) CreateClone(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error) {
	snapFull := fmt.Sprintf("%s@%s", p.dataset, snapshot)
	target := p.clonePath(cloneName)

	// 冪等性: クローンが既に存在する場合は再作成せずに成功とみなす。
	// Reconcile の再実行や Operator 再起動で重複呼び出しが起きうるため必須。
	out, cloneErr := exec.CommandContext(ctx, "zfs", "clone", snapFull, target).CombinedOutput()
	if cloneErr != nil && !strings.Contains(string(out), "dataset already exists") {
		return domain.VolumeInfo{}, fmt.Errorf("zfs clone %s -> %s: %w\n%s", snapFull, target, cloneErr, out)
	}

	// NFS エクスポートを有効化する。
	// 親 dataset の sharenfs を継承して即座にマウント可能な状態にする。
	// 失敗した場合はクローンを削除してロールバックし、設定方法を示すエラーを返す。
	if err := run(ctx, "zfs", "share", target); err != nil {
		// 既に share 済みの場合は無視する（"filesystem 'X' is already shared"）。
		// それ以外の失敗時のみロールバック。
		out2, shareErr := exec.CommandContext(ctx, "zfs", "get", "-H", "-o", "value", "sharenfs", target).Output()
		if shareErr != nil || strings.TrimSpace(string(out2)) == "off" {
			// 新規クローンを作成した場合のみロールバック（既存クローンには触らない）
			if cloneErr == nil {
				_ = run(ctx, "zfs", "destroy", "-f", target)
			}
			return domain.VolumeInfo{}, fmt.Errorf(
				"zfs share %s: %w\n"+
					"ZFS サーバーで NFS 共有を有効化してください:\n"+
					"  zfs set sharenfs=\"rw=@<k8s-pod-cidr>,no_root_squash\" %s",
				target, err, p.dataset,
			)
		}
	}

	nfsServer, _ := localIP()
	if nfsServer == "" {
		nfsServer = "127.0.0.1"
	}
	return domain.VolumeInfo{
		CloneName: cloneName,
		NFSServer: nfsServer,
		NFSPath:   "/" + target,
	}, nil
}

// DeleteClone はクローンを削除する。
// クローンが存在しない場合は zfsagent.ErrNotFound を返す。
//
// 先に NFS 共有を解除する。それでも "busy" になる場合は nfsd によるピン留めを
// 解放してから破棄する (releaseNFSAndDestroy 参照)。
func (p *AgentProvider) DeleteClone(ctx context.Context, cloneName string) error {
	target := p.clonePath(cloneName)
	_ = exec.CommandContext(ctx, "zfs", "unshare", target).Run()

	out, err := exec.CommandContext(ctx, "zfs", "destroy", "-f", target).CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.Contains(string(out), "dataset does not exist") {
		return zfsagent.ErrNotFound
	}
	if !strings.Contains(string(out), "busy") {
		return fmt.Errorf("zfs destroy %s: %w\n%s", target, err, out)
	}

	// Linux knfsd は一度クライアントへサーブしたファイルシステムを、nfsd スレッドが
	// 動いている限り参照保持する。exportfs -f / umount -f / リース失効待ちでは解放
	// されず、nfsd スレッドを止めるしかない。busy の場合のみ nfsd スレッドを一時的に
	// 0 にして参照を解放し、destroy 後にスレッドを戻して再エクスポートする。
	if rerr := releaseNFSAndDestroy(ctx, target); rerr != nil {
		return fmt.Errorf("zfs destroy %s (nfsd release): %w", target, rerr)
	}
	return nil
}

const nfsdThreadsPath = "/proc/fs/nfsd/threads"

// releaseNFSAndDestroy は nfsd スレッドを一時停止して NFS のピン留めを解放し、
// クローンを破棄してからスレッドと共有を復元する。
func releaseNFSAndDestroy(ctx context.Context, target string) error {
	prev := readNFSDThreads()
	if err := writeNFSDThreads(0); err != nil {
		return fmt.Errorf("stop nfsd threads: %w", err)
	}
	destroyErr := exec.CommandContext(ctx, "zfs", "destroy", "-f", target).Run()
	// スレッドと共有は destroy の成否にかかわらず必ず復元する。
	_ = writeNFSDThreads(prev)
	_ = exec.CommandContext(ctx, "zfs", "share", "-a").Run()
	return destroyErr
}

// readNFSDThreads は現在の nfsd スレッド数を返す。読めない場合は既定値 8 を返す。
func readNFSDThreads() int {
	b, err := os.ReadFile(nfsdThreadsPath)
	if err != nil {
		return 8
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || n <= 0 {
		return 8
	}
	return n
}

func writeNFSDThreads(n int) error {
	return os.WriteFile(nfsdThreadsPath, []byte(strconv.Itoa(n)), 0o644)
}

// ListClones はクローン一覧を返す。
func (p *AgentProvider) ListClones(ctx context.Context) ([]domain.VolumeInfo, error) {
	branchesDataset := p.dataset + "/branches"
	out, err := exec.CommandContext(ctx, "zfs", "list", "-r", "-H", "-o", "name", branchesDataset).Output()
	if err != nil {
		// branches データセットが存在しない場合は空リストを返す
		return []domain.VolumeInfo{}, nil
	}
	nfsServer, err := localIP()
	if err != nil {
		nfsServer = "127.0.0.1"
	}
	var result []domain.VolumeInfo
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if name == branchesDataset {
			continue // 親データセット自身はスキップ
		}
		// "pool/dataset/branches/cloneName" → "cloneName"
		parts := strings.SplitN(name, "/", -1)
		cloneName := parts[len(parts)-1]
		result = append(result, domain.VolumeInfo{
			CloneName: cloneName,
			NFSServer: nfsServer,
			NFSPath:   "/" + name,
		})
	}
	return result, nil
}

// GetClone はクローンの接続情報を返す。
func (p *AgentProvider) GetClone(ctx context.Context, cloneName string) (domain.VolumeInfo, error) {
	target := p.clonePath(cloneName)
	out, err := exec.CommandContext(ctx, "zfs", "list", "-H", "-o", "name", target).Output()
	if err != nil {
		return domain.VolumeInfo{}, zfsagent.ErrNotFound
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return domain.VolumeInfo{}, zfsagent.ErrNotFound
	}
	nfsServer, err := localIP()
	if err != nil {
		nfsServer = "127.0.0.1"
	}
	return domain.VolumeInfo{
		CloneName: cloneName,
		NFSServer: nfsServer,
		NFSPath:   "/" + target,
	}, nil
}

func (p *AgentProvider) clonePath(cloneName string) string {
	return fmt.Sprintf("%s/branches/%s", p.dataset, cloneName)
}

// GCSnapshots はアーカイブスナップショットをクリーンアップする。
// {prefix}-YYYYMMDD-HHMMSS 形式のスナップショットを prefix ごとにグループ化し、
// keepCount 件を超えた古いものかつ依存クローンがないものを削除する。
func (p *AgentProvider) GCSnapshots(ctx context.Context, keepCount int) ([]string, error) {
	snaps, err := p.ListSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	// アーカイブパターンでグループ化: prefix → []SnapshotInfo（新しい順）
	groups := map[string][]domain.SnapshotInfo{}
	for _, s := range snaps {
		if len(s.Name) >= 16 {
			suffix := s.Name[len(s.Name)-16:]
			if suffix[0] == '-' && suffix[9] == '-' && isAllDigits(suffix[1:9]) && isAllDigits(suffix[10:16]) {
				prefix := s.Name[:len(s.Name)-16]
				groups[prefix] = append(groups[prefix], s)
			}
		}
	}

	var deleted []string
	for _, archives := range groups {
		// CreatedAt 降順（新しい順）にソート
		sortSnapshotsByCreatedAtDesc(archives)
		// keepCount 件を超えた古いものを削除候補に
		if len(archives) <= keepCount {
			continue
		}
		for _, s := range archives[keepCount:] {
			// 依存クローンを確認（zfs get clones）
			zfsName := fmt.Sprintf("%s@%s", p.dataset, s.Name)
			out, _ := exec.CommandContext(ctx, "zfs", "get", "-H", "-o", "value", "clones", zfsName).Output()
			if strings.TrimSpace(string(out)) != "" && strings.TrimSpace(string(out)) != "-" {
				continue // 依存クローンあり → スキップ
			}
			if err := p.DeleteSnapshot(ctx, s.Name); err != nil {
				continue // 削除失敗はスキップして続行
			}
			deleted = append(deleted, s.Name)
		}
	}
	return deleted, nil
}

func isAllDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return len(s) > 0
}

func sortSnapshotsByCreatedAtDesc(snaps []domain.SnapshotInfo) {
	for i := 0; i < len(snaps); i++ {
		for j := i + 1; j < len(snaps); j++ {
			if snaps[j].CreatedAt.After(snaps[i].CreatedAt) {
				snaps[i], snaps[j] = snaps[j], snaps[i]
			}
		}
	}
}

// ResetDataset はすべてのクローンとスナップショットを削除してデータセットをリセットする。
func (p *AgentProvider) ResetDataset(ctx context.Context) error {
	// 1. 全クローンを削除
	clones, err := p.ListClones(ctx)
	if err != nil {
		return fmt.Errorf("reset: list clones: %w", err)
	}
	for _, c := range clones {
		if err := p.DeleteClone(ctx, c.CloneName); err != nil {
			return fmt.Errorf("reset: delete clone %q: %w", c.CloneName, err)
		}
	}
	// 2. 全スナップショットを削除
	snaps, err := p.ListSnapshots(ctx)
	if err != nil {
		return fmt.Errorf("reset: list snapshots: %w", err)
	}
	for _, s := range snaps {
		if err := p.DeleteSnapshot(ctx, s.Name); err != nil {
			return fmt.Errorf("reset: delete snapshot %q: %w", s.Name, err)
		}
	}
	return nil
}

// parseSnapshotList は "zfs list -p -t snapshot" の出力をパースする。
// -p フラグにより creation フィールドは epoch 秒（整数）として出力される。
func parseSnapshotList(out []byte, dataset string) ([]domain.SnapshotInfo, error) {
	var result []domain.SnapshotInfo
	prefix := dataset + "@"
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		fullName := fields[0]
		name := strings.TrimPrefix(fullName, prefix)
		var createdAt time.Time
		if epochSec, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			createdAt = time.Unix(epochSec, 0).UTC()
		}
		result = append(result, domain.SnapshotInfo{
			Name:      name,
			CreatedAt: createdAt,
		})
	}
	return result, nil
}

// localIP はホストの主要な非ループバック IP アドレスを返す。
func localIP() (string, error) {
	out, err := exec.Command("hostname", "-I").Output()
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("no IP found")
	}
	return fields[0], nil
}
