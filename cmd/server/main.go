package main

import (
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"ps-trophy-ranking/internal/config"
	httpserver "ps-trophy-ranking/internal/http"
	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/psn"
	"ps-trophy-ranking/internal/trophy"
)

func main() {
	cfg := config.Load()

	log.Printf("Starting PSN Trophy Leaderboard server")
	log.Printf("Data source: %s", cfg.PSNMode)
	log.Printf("Database: %s", cfg.DBPath)
	log.Printf("Listen address: %s", cfg.ListenAddr)

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	store, err := player.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open player store: %v", err)
	}
	defer store.Close()

	var source trophy.Source
	var dataSourceLabel string

	switch cfg.PSNMode {
	case "psn":
		if cfg.PSNNPSSO == "" {
			log.Println("Warning: PSN mode selected but PSN_NPSSO not set")
		}
		client := psn.New(cfg.PSNNPSSO)
		source = trophy.NewPSN(client, 15*time.Minute)
		dataSourceLabel = "PSN 真实档案"
	case "fixture":
		source = trophy.NewFixture()
		dataSourceLabel = "演示数据"
	default:
		log.Fatalf("invalid PSN_MODE: %s (must be 'fixture' or 'psn')", cfg.PSNMode)
	}

	templatesFS, staticFS := getWebFS()

	server, err := httpserver.New(store, source, dataSourceLabel, templatesFS, staticFS)
	if err != nil {
		log.Fatalf("create HTTP server: %v", err)
	}

	log.Printf("Server listening on http://%s", cfg.ListenAddr)
	log.Printf("Open http://%s in your browser", cfg.ListenAddr)

	if err := http.ListenAndServe(cfg.ListenAddr, server.Handler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func getWebFS() (templatesFS, staticFS fs.FS) {
	templatesPath := "web/templates"
	staticPath := "web/static"
	
	if _, err := os.Stat(templatesPath); err == nil {
		templatesFS = os.DirFS(templatesPath)
		staticFS = os.DirFS(staticPath)
		log.Printf("Using web files from disk")
		return
	}
	
	log.Fatalf("web/templates and web/static directories not found")
	return
}
