package main

import (
	"os"
	"testing"
)

func TestEnvOr(t *testing.T) {
	const key = "ZFSDB_TEST_KEY_ENVDOR"
	os.Unsetenv(key)

	if got := envOr(key, "fallback"); got != "fallback" {
		t.Errorf("envOr() = %q, want %q", got, "fallback")
	}

	t.Setenv(key, "set-value")
	if got := envOr(key, "fallback"); got != "set-value" {
		t.Errorf("envOr() = %q, want %q", got, "set-value")
	}
}
