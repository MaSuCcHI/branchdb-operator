package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// E2E では Mac → Colima VM の kubectl port-forward 経由で API に接続するため、
// idle TCP セッションが切れて EOF が返ることがある。各リクエストはリトライ可能にする。
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives: true, // 毎回新規接続でセッション切断を回避
	},
}

func get(ctx context.Context, url string) (map[string]any, error) {
	var lastErr error
	for i := 0; i < 5; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		var out map[string]any
		return out, json.NewDecoder(resp.Body).Decode(&out)
	}
	return nil, lastErr
}

func post(ctx context.Context, url string, body any) (map[string]any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for i := 0; i < 5; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body2, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body2))
		}
		var out map[string]any
		return out, json.NewDecoder(resp.Body).Decode(&out)
	}
	return nil, lastErr
}

func del(ctx context.Context, url string) error {
	var lastErr error
	for i := 0; i < 5; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
		if err != nil {
			return err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
		}
		return nil
	}
	return lastErr
}

func branchURL(name string) string {
	return fmt.Sprintf("%s/branches/%s", apiURL(), name)
}
