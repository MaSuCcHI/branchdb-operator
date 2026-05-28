package zfsagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
func (p *Provider) TakeSnapshot(ctx context.Context, name string) error {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return fmt.Errorf("zfsagent: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/snapshots", bytes.NewReader(body))
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
func (p *Provider) CreateClone(ctx context.Context, snapshot, cloneName string) (domain.VolumeInfo, error) {
	body, err := json.Marshal(map[string]string{
		"snapshot": snapshot,
		"name":     cloneName,
	})
	if err != nil {
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/clones", bytes.NewReader(body))
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
		return domain.VolumeInfo{}, fmt.Errorf("zfsagent: CreateClone: unexpected status %d", resp.StatusCode)
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
func (p *Provider) DeleteClone(ctx context.Context, cloneName string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.baseURL+"/clones/"+cloneName, nil)
	if err != nil {
		return fmt.Errorf("zfsagent: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("zfsagent: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("zfsagent: DeleteClone: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListSnapshots は ZFS Agent からスナップショット一覧を取得する。
func (p *Provider) ListSnapshots(ctx context.Context) ([]domain.SnapshotInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/snapshots", nil)
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
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
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
			Name:      r.Name,
			CreatedAt: t,
		})
	}
	return snaps, nil
}
