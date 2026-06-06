package main

import (
	"os"
	"testing"
)

func TestParseDatasets(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		want  map[string]string
		empty bool
	}{
		{
			name:  "empty string",
			raw:   "",
			empty: true,
		},
		{
			name: "single dataset",
			raw:  "mysql:tank/mysql",
			want: map[string]string{"mysql": "tank/mysql"},
		},
		{
			name: "multiple datasets",
			raw:  "mysql:tank/mysql,postgres:tank/postgres",
			want: map[string]string{"mysql": "tank/mysql", "postgres": "tank/postgres"},
		},
		{
			name: "with spaces",
			raw:  " mysql : tank/mysql , postgres : tank/postgres ",
			want: map[string]string{},
		},
		{
			name:  "missing colon",
			raw:   "invalidentry",
			empty: true,
		},
		{
			name:  "empty key",
			raw:   ":tank/mysql",
			empty: true,
		},
		{
			name:  "empty value",
			raw:   "mysql:",
			empty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDatasets(tt.raw)
			if tt.empty {
				if len(got) != 0 {
					t.Errorf("expected empty map, got %v", got)
				}
				return
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseDatasets(%q)[%q] = %q, want %q", tt.raw, k, got[k], v)
				}
			}
		})
	}
}

func TestEnvOr(t *testing.T) {
	const key = "ZFSAGENT_TEST_KEY_ENVDOR"
	os.Unsetenv(key)

	if got := envOr(key, "default"); got != "default" {
		t.Errorf("envOr() = %q, want %q", got, "default")
	}

	t.Setenv(key, "override")
	if got := envOr(key, "default"); got != "override" {
		t.Errorf("envOr() = %q, want %q", got, "override")
	}
}

func TestLoadConfig_DefaultDataset(t *testing.T) {
	os.Unsetenv("ZFSAGENT_DATASETS")
	t.Setenv("ZFSAGENT_POOL", "mypool")
	t.Setenv("ZFSAGENT_DATASET", "mydb")
	t.Setenv("ZFSAGENT_ADDR", ":9999")
	t.Setenv("ZFSAGENT_TOKEN", "tok123")

	cfg := loadConfig()
	if cfg.Addr != ":9999" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
	if cfg.Token != "tok123" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.Datasets["mysql"] != "mypool/mydb" {
		t.Errorf("Datasets[mysql] = %q, want %q", cfg.Datasets["mysql"], "mypool/mydb")
	}
}

func TestLoadConfig_MultiDataset(t *testing.T) {
	t.Setenv("ZFSAGENT_DATASETS", "mysql:tank/mysql,postgres:tank/postgres")
	t.Setenv("ZFSAGENT_TOKEN", "tok")

	cfg := loadConfig()
	if cfg.Datasets["mysql"] != "tank/mysql" {
		t.Errorf("Datasets[mysql] = %q", cfg.Datasets["mysql"])
	}
	if cfg.Datasets["postgres"] != "tank/postgres" {
		t.Errorf("Datasets[postgres] = %q", cfg.Datasets["postgres"])
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	os.Unsetenv("ZFSAGENT_ADDR")
	os.Unsetenv("ZFSAGENT_DATASETS")
	os.Unsetenv("ZFSAGENT_POOL")
	os.Unsetenv("ZFSAGENT_DATASET")
	os.Unsetenv("ZFSAGENT_TOKEN")

	cfg := loadConfig()
	if cfg.Addr != ":9090" {
		t.Errorf("default Addr = %q, want :9090", cfg.Addr)
	}
	if cfg.Datasets["mysql"] != "tank/mysql" {
		t.Errorf("default dataset = %q, want tank/mysql", cfg.Datasets["mysql"])
	}
}

func TestBuildProviders(t *testing.T) {
	cfg := config{
		Datasets: map[string]string{
			"mysql":    "tank/mysql",
			"postgres": "tank/postgres",
		},
	}
	providers := buildProviders(cfg)
	if len(providers) != 2 {
		t.Errorf("len(providers) = %d, want 2", len(providers))
	}
	if providers["mysql"] == nil {
		t.Error("providers[mysql] is nil")
	}
	if providers["postgres"] == nil {
		t.Error("providers[postgres] is nil")
	}
}

func TestBuildProviders_Empty(t *testing.T) {
	providers := buildProviders(config{Datasets: map[string]string{}})
	if len(providers) != 0 {
		t.Errorf("expected empty map, got %d entries", len(providers))
	}
}

func TestRun_EmptyToken_ReturnsError(t *testing.T) {
	os.Unsetenv("ZFSAGENT_TOKEN")
	os.Unsetenv("ZFSAGENT_DATASETS")
	os.Unsetenv("ZFSAGENT_POOL")
	os.Unsetenv("ZFSAGENT_DATASET")

	err := run()
	if err == nil {
		t.Fatal("expected error when ZFSAGENT_TOKEN is empty")
	}
}
