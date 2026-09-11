package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	os.Clearenv()
	cfg := Load()

	if cfg.PSNNPSSO != "" {
		t.Errorf("PSNNPSSO = %q, want empty", cfg.PSNNPSSO)
	}
	if cfg.DBPath != "./data/leaderboard.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "./data/leaderboard.db")
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:8080")
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Clearenv()
	os.Setenv("PSN_NPSSO", "test-npsso")
	os.Setenv("DB_PATH", "/tmp/test.db")
	os.Setenv("LISTEN_ADDR", "0.0.0.0:9090")

	cfg := Load()

	if cfg.PSNNPSSO != "test-npsso" {
		t.Errorf("PSNNPSSO = %q, want %q", cfg.PSNNPSSO, "test-npsso")
	}
	if cfg.DBPath != "/tmp/test.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/tmp/test.db")
	}
	if cfg.ListenAddr != "0.0.0.0:9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:9090")
	}
}

func TestLoadDotEnv(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")

	content := `
# Comment line
PSN_NPSSO="my-npsso-value"
DB_PATH=/custom/path.db

`
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.Clearenv()
	cfg := Load()

	if cfg.PSNNPSSO != "my-npsso-value" {
		t.Errorf("PSNNPSSO = %q, want %q", cfg.PSNNPSSO, "my-npsso-value")
	}
	if cfg.DBPath != "/custom/path.db" {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, "/custom/path.db")
	}
}

func TestEnvOverridesDotEnv(t *testing.T) {
	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")

	content := "PSN_NPSSO=dotenv-value\n"
	if err := os.WriteFile(envFile, []byte(content), 0600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	os.Clearenv()
	os.Setenv("PSN_NPSSO", "env-value")

	cfg := Load()

	if cfg.PSNNPSSO != "env-value" {
		t.Errorf("PSNNPSSO = %q, want %q (env should override .env)", cfg.PSNNPSSO, "env-value")
	}
}
