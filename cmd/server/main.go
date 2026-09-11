package main

import (
	"io/fs"
	"log/slog"
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

	slog.Info("Starting PSN Trophy Leaderboard server")
	slog.Info("Configuration loaded", 
		slog.String("db_path", cfg.DBPath),
		slog.String("listen_addr", cfg.ListenAddr),
		slog.String("wal_dir", cfg.WALDir))

	if cfg.PSNNPSSO == "" {
		slog.Warn("PSN_NPSSO not set, /join endpoint will return 'no_credentials' error")
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0755); err != nil {
		slog.Error("Failed to create data directory", slog.String("error", err.Error()))
		os.Exit(1)
	}

	store, err := player.Open(cfg.DBPath)
	if err != nil {
		slog.Error("Failed to open player store", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer store.Close()

	client := psn.New(cfg.PSNNPSSO)
	source := trophy.NewPSN(client)

	// Phase 2: 初始化 WAL Manager
	walMgr, err := wal.New(cfg.WALDir, 10)
	if err != nil {
		slog.Error("Failed to create WAL manager", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer walMgr.Close()

	templatesFS, staticFS := getWebFS()

	server, err := httpserver.New(store, source, walMgr, templatesFS, staticFS)
	if err != nil {
		slog.Error("Failed to create HTTP server", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Phase 2: 启动时重放 sealed 段
	slog.Info("Replaying sealed WAL segments...")
	if err := walMgr.ReplaySealed(func(players []player.Player) error {
		// 批量刷盘到 SQLite
		for _, p := range players {
			if err := store.Upsert(p); err != nil {
				return err
			}
		}
		// 从 store 读取完整数据（含 JoinedAt）再批量更新内存
		storedPlayers := make([]player.Player, 0, len(players))
		for _, p := range players {
			stored, err := store.Get(p.OnlineID)
			if err == nil && stored != nil {
				storedPlayers = append(storedPlayers, *stored)
			} else {
				storedPlayers = append(storedPlayers, p)
			}
		}
		server.UpsertMemoryBatch(storedPlayers)
		return nil
	}); err != nil {
		slog.Error("Failed to replay sealed segments", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Phase 2: 启动封段 Worker
	walMgr.StartSealing(cfg.WALSealIntervalMS, func(players []player.Player) error {
		// 批量刷盘到 SQLite
		for _, p := range players {
			if err := store.Upsert(p); err != nil {
				return err
			}
		}
		// 从 store 读取完整数据（含 JoinedAt）再批量更新内存
		storedPlayers := make([]player.Player, 0, len(players))
		for _, p := range players {
			stored, err := store.Get(p.OnlineID)
			if err == nil && stored != nil {
				storedPlayers = append(storedPlayers, *stored)
			} else {
				storedPlayers = append(storedPlayers, p)
			}
		}
		server.UpsertMemoryBatch(storedPlayers)
		return nil
	})

	slog.Info("WAL sealing worker started", slog.Int("interval_ms", cfg.WALSealIntervalMS))

	slog.Info("Server listening",
		slog.String("addr", cfg.ListenAddr),
		slog.String("url", "http://"+cfg.ListenAddr),
		slog.String("pprof", "http://"+cfg.ListenAddr+"/debug/pprof/"))

	if err := http.ListenAndServe(cfg.ListenAddr, server.Handler()); err != nil {
		slog.Error("Server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func getWebFS() (templatesFS, staticFS fs.FS) {
	templatesPath := "web/templates"
	staticPath := "web/static"
	
	if _, err := os.Stat(templatesPath); err == nil {
		templatesFS = os.DirFS(templatesPath)
		staticFS = os.DirFS(staticPath)
		slog.Info("Using web files from disk")
		return
	}
	
	slog.Error("web/templates and web/static directories not found")
	os.Exit(1)
	return
}
