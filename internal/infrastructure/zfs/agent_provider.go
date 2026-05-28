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

	"github.com/keisuke/zfs-db-k8s/internal/domain"
	"github.com/keisuke/zfs-db-k8s/internal/interface/api/zfsagent"
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
// name は "snap-20260526" のようなスナップショット名のみ（@ 不要）。
func (p *AgentProvider) TakeSnapshot(ctx context.Context, name string) error {
	zfsName := fmt.Sprintf("%s@%s", p.dataset, name)
	return run(ctx, "zfs", "snapshot", zfsName)
}

// ListSnapshots はスナップショット一覧を返す。
func (p *AgentProvider) ListSnapshots(ctx context.Context) ([]domain.SnapshotInfo, error) {
	out, err := exec.CommandContext(ctx, "zfs", "list", "-t", "snapshot", "-H", "-o", "name,creation", p.dataset).Output()
	if err != nil {
		return nil, fmt.Errorf("zfs list snapshots: %w", err)
	}
	return parseSnapshotList(out, p.dataset)
}

// CreateClone は指定スナップショットからクローンを作成し、接続情報を返す。
func (p *AgentProvider) CreateClone(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error) {
	snapFull := fmt.Sprintf("%s@%s", p.dataset, snapshot)
	target := p.clonePath(cloneName)
	if err := run(ctx, "zfs", "clone", snapFull, target); err != nil {
		return domain.VolumeInfo{}, err
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
	out, err := exec.CommandContext(ctx, "zfs", "list", "-H", "-o", "name", branchesDataset).Output()
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

// parseSnapshotList は "zfs list -t snapshot" の出力をパースする。
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
		createdAt, _ := time.Parse("Mon Jan  2 15:04 2006", strings.Join(fields[1:], " "))
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
