package main

import (
	"os"
	"testing"
)

func TestGetEnv(t *testing.T) {
	const key = "ZFSDB_TEST_KEY_GETENV"
	os.Unsetenv(key)

	if got := getEnv(key, "default-val"); got != "default-val" {
		t.Errorf("getEnv() = %q, want %q", got, "default-val")
	}

	t.Setenv(key, "actual")
	if got := getEnv(key, "default-val"); got != "actual" {
		t.Errorf("getEnv() = %q, want %q", got, "actual")
	}
}
