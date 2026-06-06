package api

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"testing"
)

// --- 最小限のテスト用 SQL ドライバ ---

type fakeDriver struct{ rows [][]driver.Value }

type fakeConn struct{ rows [][]driver.Value }
type fakeStmt struct{ rows [][]driver.Value }
type fakeRows struct {
	rows [][]driver.Value
	idx  int
}
type fakeTx struct{}

func (d *fakeDriver) Open(_ string) (driver.Conn, error) { return &fakeConn{rows: d.rows}, nil }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{rows: c.rows}, nil
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return &fakeTx{}, nil }
func (t *fakeTx) Commit() error               { return nil }
func (t *fakeTx) Rollback() error             { return nil }

func (s *fakeStmt) Close() error                                 { return nil }
func (s *fakeStmt) NumInput() int                                { return 0 }
func (s *fakeStmt) Exec(_ []driver.Value) (driver.Result, error) { return nil, nil }
func (s *fakeStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return &fakeRows{rows: s.rows}, nil
}

func (r *fakeRows) Columns() []string { return []string{"Variable_name", "Value"} }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

// --- テスト ---

func TestRealMySQLQuerier_クエリ成功時にスレッド数を返す(t *testing.T) {
	driverName := fmt.Sprintf("fakemysql-%s", t.Name())
	sql.Register(driverName, &fakeDriver{rows: [][]driver.Value{
		{"Threads_connected", "7"},
	}})

	// realMySQLQuerier を直接テストするため dsn の driver 部分をすり替える。
	// realMySQLQuerier は内部で sql.Open("mysql", dsn) を呼ぶため、
	// driverName を使うラッパーを直接呼び出す。
	threads, err := queryMySQLThreads(context.Background(), driverName, "dummy-dsn")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if threads != 7 {
		t.Errorf("threads = %d, want 7", threads)
	}
}

func TestRealMySQLQuerier_行が0件のとき0を返す(t *testing.T) {
	driverName := fmt.Sprintf("fakemysql-%s-empty", t.Name())
	sql.Register(driverName, &fakeDriver{rows: nil}) // no rows

	threads, err := queryMySQLThreads(context.Background(), driverName, "dummy-dsn")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if threads != 0 {
		t.Errorf("threads = %d, want 0", threads)
	}
}
