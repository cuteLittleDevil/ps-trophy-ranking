package main

import (
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"ps-trophy-ranking/internal/config"
	httpserver "ps-trophy-ranking/internal/http"
	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/psn"
	"ps-trophy-ranking/internal/trophy"
	"ps-trophy-ranking/internal/wal"
)

func main() {
	cfg := config.Load()

	log.Printf("Starting PSN Trophy Leaderboard server")
	log.Printf("Database: %s", cfg.DBPath)
	log.Printf("Listen address: %s", cfg.ListenAddr)
	log.Printf("WAL directory: %s", cfg.WALDir)

	if cfg.PSNNPSSO == "" {
		log.Println("Warning: PSN_NPSSO not set. /join endpoint will return 'no_credentials' error.")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	store, err := player.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open player store: %v", err)
	}
	defer store.Close()

	client := psn.New(cfg.PSNNPSSO)
	source := trophy.NewPSN(client)

	// Phase 2: 初始化 WAL Manager
	walMgr, err := wal.New(cfg.WALDir, 10)
	if err != nil {
		log.Fatalf("create wal manager: %v", err)
	}
	defer walMgr.Close()

	templatesFS, staticFS := getWebFS()

	server, err := httpserver.New(store, source, walMgr, templatesFS, staticFS)
	if err != nil {
		log.Fatalf("create HTTP server: %v", err)
	}

	// Phase 2: 启动时重放 sealed 段
	log.Println("Replaying sealed WAL segments...")
	if err := walMgr.ReplaySealed(func(players []player.Player) error {
		// 批量刷盘到 SQLite 并更新内存
		for _, p := range players {
			if err := store.Upsert(p); err != nil {
				return err
			}
			// 从 store 读取完整数据（含 JoinedAt）再更新内存
			stored, err := store.Get(p.OnlineID)
			if err == nil && stored != nil {
				server.UpsertMemory(*stored)
			} else {
				server.UpsertMemory(p)
			}
		}
		return nil
	}); err != nil {
		log.Fatalf("replay sealed segments: %v", err)
	}

	// Phase 2: 启动封段 Worker
	walMgr.StartSealing(cfg.WALSealIntervalMS, func(players []player.Player) error {
		// 批量刷盘到 SQLite 并更新内存
		for _, p := range players {
			if err := store.Upsert(p); err != nil {
				return err
			}
			// 从 store 读取完整数据（含 JoinedAt）再更新内存
			stored, err := store.Get(p.OnlineID)
			if err == nil && stored != nil {
				server.UpsertMemory(*stored)
			} else {
				server.UpsertMemory(p)
			}
		}
		return nil
	})

	log.Printf("WAL sealing worker started (interval: %dms)", cfg.WALSealIntervalMS)

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
