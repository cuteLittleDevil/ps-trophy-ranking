package config

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// Config holds server configuration.
type Config struct {
	// PSNNPSSO is the Sony NPSSO cookie. Required for /join endpoint.
	PSNNPSSO string
	// DBPath is the SQLite database file path. Default: "./data/leaderboard.db".
	DBPath string
	// ListenAddr is the HTTP listen address. Default: "127.0.0.1:8080".
	ListenAddr string
	// WALDir is the WAL directory path. Default: "./data/wal/".
	WALDir string
	// WALSealIntervalMS is the WAL seal interval in milliseconds. Default: 100.
	WALSealIntervalMS int
}

// Load reads configuration from environment variables and optional .env file.
func Load() Config {
	loadDotEnv()

	walSealInterval := 100
	if val := os.Getenv("WAL_SEAL_INTERVAL_MS"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			walSealInterval = parsed
		}
	}

	cfg := Config{
		PSNNPSSO:          os.Getenv("PSN_NPSSO"),
		DBPath:            getEnv("DB_PATH", "./data/leaderboard.db"),
		ListenAddr:        getEnv("LISTEN_ADDR", "127.0.0.1:8080"),
		WALDir:            getEnv("WAL_DIR", "./data/wal/"),
		WALSealIntervalMS: walSealInterval,
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

// loadDotEnv reads .env file in the current directory if it exists.
// Does not override existing environment variables.
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}
