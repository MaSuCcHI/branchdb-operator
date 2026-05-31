package zfsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/MaSuCcHI/branchdb-operator/internal/domain"
)

// Provider は ZFS Agent への HTTP クライアント。VolumeProvider を実装する。
//
// 設定は構造体フィールドで渡す:
//
//	BaseURL string // "http://zfs-server:9090"
//	Token   string // Bearer トークン
type Provider struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewProvider は Provider を生成する。
func NewProvider(baseURL, token string) *Provider {
	return &Provider{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// WithHTTPClient は Provider の HTTP クライアントを差し替えてテストを容易にする。
func (p *Provider) WithHTTPClient(c *http.Client) *Provider {
	p.httpClient = c
	return p
}

// TakeSnapshot は ZFS Agent にスナップショット作成を要求する。
func (p *Provider) TakeSnapshot(ctx context.Context, dbType, name string) error {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return fmt.Errorf("zfsagent: marshal request: %w", err)
	}

	url := p.baseURL + "/snapshots" + dbTypeQuery(dbType)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("zfsagent: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("zfsagent: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("zfsagent: TakeSnapshot: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// CreateClone は ZFS Agent にクローン作成を要求し、VolumeInfo を返す。
func (p *Provider) CreateClone(ctx context.Context, dbType, snapshot, cloneName string) (domain.VolumeInfo, error) {
	body, err := json.Marshal(map[string]string{
		"snapshot": snapshot,
		"name":     cloneName,
	})
	if err != nil {
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/clones"+dbTypeQuery(dbType), bytes.NewReader(body))
	if err != nil {
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: CreateClone: unexpected status %d: %s", resp.StatusCode, bytes.TrimSpace(respBody))
	}

	var result struct {
		CloneName string `json:"clone_name"`
		NFSServer string `json:"nfs_server"`
		NFSPath   string `json:"nfs_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: decode response: %w", err)
	}

	return domain.VolumeInfo{
		CloneName: result.CloneName,
		NFSServer: result.NFSServer,
		NFSPath:   result.NFSPath,
	}, nil
}

// DeleteClone は ZFS Agent にクローン削除を要求する。
func (p *Provider) DeleteClone(ctx context.Context, dbType, cloneName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+"/clones/"+cloneName+dbTypeQuery(dbType), nil)
	if err != nil {
		return fmt.Errorf("zfsagent: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("zfsagent: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// クローンが存在しない場合は削除済みとみなして成功とする（冪等性）。
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("zfsagent: DeleteClone: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListSnapshots は ZFS Agent からスナップショット一覧を取得する。
func (p *Provider) ListSnapshots(ctx context.Context, dbType string) ([]domain.SnapshotInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/snapshots"+dbTypeQuery(dbType), nil)
	if err != nil {
		return nil, fmt.Errorf("zfsagent: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zfsagent: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("zfsagent: ListSnapshots: unexpected status %d", resp.StatusCode)
	}

	var raw []struct {
		Name         string `json:"name"`
		CreatedAt    string `json:"created_at"`
		DatabaseType string `json:"database_type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("zfsagent: decode response: %w", err)
	}

	snaps := make([]domain.SnapshotInfo, 0, len(raw))
	for _, r := range raw {
		t, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("zfsagent: parse created_at %q: %w", r.CreatedAt, err)
		}
		snaps = append(snaps, domain.SnapshotInfo{
			Name:         r.Name,
			CreatedAt:    t,
			DatabaseType: r.DatabaseType,
		})
	}
	return snaps, nil
}

// dbTypeQuery は dbType が空でない場合に "?db_type=<dbType>" を返す。
func dbTypeQuery(dbType string) string {
	if dbType == "" {
		return ""
	}
	return "?db_type=" + dbType
}
