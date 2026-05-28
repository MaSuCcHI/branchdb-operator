package zfs

import (
	"context"
	"fmt"
	"os/exec"
)

type Client struct {
	dataset string // e.g. "tank/mysql"
}

func NewClient(dataset string) *Client {
	return &Client{dataset: dataset}
}

func (c *Client) Clone(ctx context.Context, zfsName string, branchName string) error {
	target := fmt.Sprintf("%s/branches/%s", c.dataset, branchName)
	return run(ctx, "zfs", "clone", zfsName, target)
}

func (c *Client) Promote(ctx context.Context, branchName string) error {
	target := fmt.Sprintf("%s/branches/%s", c.dataset, branchName)
	return run(ctx, "zfs", "promote", target)
}

func (c *Client) DestroyClone(ctx context.Context, branchName string) error {
	target := fmt.Sprintf("%s/branches/%s", c.dataset, branchName)
	return run(ctx, "zfs", "destroy", target)
}

func (c *Client) DestroySnapshot(ctx context.Context, zfsName string) error {
	return run(ctx, "zfs", "destroy", zfsName)
}

func (c *Client) CreateSnapshot(ctx context.Context, zfsName string) error {
	return run(ctx, "zfs", "snapshot", zfsName)
}

// Rollback rolls back the dataset to the given snapshot.
// The -r flag destroys any snapshots (and dependent clones) more recent than the target.
func (c *Client) Rollback(ctx context.Context, zfsName string) error {
	return run(ctx, "zfs", "rollback", "-r", zfsName)
}

func run(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w\n%s", name, args, err, out)
	}
	return nil
}
