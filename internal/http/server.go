package http

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"ps-trophy-ranking/internal/memrank"
	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/rank"
	"ps-trophy-ranking/internal/trophy"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	pageSize       = 50
	cookieName     = "online_id"
	cookieMaxAge   = 2592000 // 30 days
	minIDLength    = 3
	maxIDLength    = 16
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Server is the HTTP server with dependencies.
type Server struct {
	store       *player.Store
	source      trophy.Source
	leaderboard *memrank.Leaderboard
	tmpl        *template.Template
	templatesFS fs.FS
	staticFS    fs.FS
}

// New creates a new HTTP server and loads the initial leaderboard from store.
func New(store *player.Store, source trophy.Source, templatesFS, staticFS fs.FS) (*Server, error) {
	funcMap := template.FuncMap{
		"add":        func(a, b int) int { return a + b },
		"sub":        func(a, b int) int { return a - b },
		"gt":         func(a, b int) bool { return a > b },
		"lt":         func(a, b int) bool { return a < b },
		"hasPrefix":  func(s, prefix string) bool { return strings.HasPrefix(s, prefix) },
		"trimPrefix": func(s, prefix string) string { return strings.TrimPrefix(s, prefix) },
		"slice": func(s string, start, end int) string {
			if len(s) == 0 {
				return "?"
			}
			if start >= len(s) {
				return "?"
			}
			if end > len(s) {
				end = len(s)
			}
			return s[start:end]
		},
		"toUpper": func(s string) string { return strings.ToUpper(s) },
	}
	
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	// 启动时从 SQLite 加载全量玩家到内存
	leaderboard := memrank.New()
	players, err := store.ListAll()
	if err != nil {
		return nil, fmt.Errorf("load initial leaderboard: %w", err)
	}
	log.Printf("Loading %d players into memory leaderboard", len(players))
	leaderboard.Load(players)
	log.Printf("Memory leaderboard initialized: %d players, threshold score: %d", 
		leaderboard.Count(), leaderboard.ThresholdScore())

	return &Server{
		store:       store,
		source:      source,
		leaderboard: leaderboard,
		tmpl:        tmpl,
		templatesFS: templatesFS,
		staticFS:    staticFS,
	}, nil
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	
	// Middleware
	r.Use(middleware.Recoverer)
	
	// Routes
	r.Get("/", s.handleHome)
	r.Post("/join", s.handleJoin)
	r.Post("/refresh", s.handleRefresh)
	r.Get("/search", s.handleSearch)
	r.Get("/me", s.handleMe)
	r.Post("/admin/seed", s.handleAdminSeed)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(s.staticFS))))
	
	return r
}

type pageData struct {
	Players      []rankedPlayerView
	CurrentPage  int
	TotalPages   int
	Error        string
	InfoMessage  string
	EmptyMessage string
	InputID      string
}

type rankedPlayerView struct {
	rank.RankedPlayer
	IsYou bool
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	highlight := strings.TrimSpace(r.URL.Query().Get("highlight"))

	// Phase 1: 从内存读取，不再每次 ListAll + SortAndNumber
	count := s.leaderboard.Count()
	if count == 0 {
		s.render(w, pageData{
			EmptyMessage: "还没有玩家入榜",
		})
		return
	}

	totalPages := s.leaderboard.TotalPages(pageSize)

	if page > totalPages && totalPages > 0 {
		s.render(w, pageData{
			EmptyMessage: "没有更多玩家",
			CurrentPage:  page,
			TotalPages:   totalPages,
		})
		return
	}

	// 从内存分页（Top1000 或全量）
	paged := s.leaderboard.Page(page, pageSize)
	views := make([]rankedPlayerView, len(paged))
	for i, p := range paged {
		views[i] = rankedPlayerView{
			RankedPlayer: p,
			IsYou:        highlight != "" && strings.EqualFold(p.OnlineID, highlight),
		}
	}

	s.render(w, pageData{
		Players:     views,
		CurrentPage: page,
		TotalPages:  totalPages,
	})
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	onlineID := strings.TrimSpace(r.FormValue("online_id"))

	if onlineID == "" {
		s.renderError(w, "请输入 PSN Online ID", "")
		return
	}

	if err := validateOnlineID(onlineID); err != nil {
		s.renderError(w, err.Error(), onlineID)
		return
	}

	// Check store-based cooldown for PSN syncs
	existing, err := s.store.Get(onlineID)
	if err != nil {
		log.Printf("check cooldown: %v", err)
	}
	if existing != nil && time.Since(existing.SyncedAt) < 15*time.Minute {
		s.renderError(w, "同步过于频繁", onlineID)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	summary, err := s.source.Lookup(ctx, onlineID)
	if err != nil {
		msg := mapTrophyError(err)
		s.renderError(w, msg, onlineID)
		return
	}

	// Reject negative trophy counts
	if summary.Bronze < 0 || summary.Silver < 0 || summary.Gold < 0 || summary.Platinum < 0 {
		s.renderError(w, "暂时无法同步奖杯，请稍后重试", onlineID)
		return
	}

	// Validate avatar URL: accept https, or http from Sony CDN
	avatarURL := summary.AvatarURL
	if avatarURL != "" {
		if strings.HasPrefix(avatarURL, "http://") {
			// Upgrade Sony CDN URLs from http to https
			if strings.Contains(avatarURL, "static-resource.np.community.playstation.net") {
				avatarURL = strings.Replace(avatarURL, "http://", "https://", 1)
			} else {
				// Reject other http URLs
				avatarURL = ""
			}
		} else if !strings.HasPrefix(avatarURL, "https://") {
			// Reject non-http(s) schemes
			avatarURL = ""
		}
	}

	p := player.Player{
		OnlineID:  summary.OnlineID,
		DisplayID: summary.DisplayID,
		AvatarURL: avatarURL,
		Bronze:    summary.Bronze,
		Silver:    summary.Silver,
		Gold:      summary.Gold,
		Platinum:  summary.Platinum,
		Score:     rank.Score(summary.Bronze, summary.Silver, summary.Gold, summary.Platinum),
		SyncedAt:  time.Now(),
	}

	// Phase 1: 同步 Upsert SQLite
	if err := s.store.Upsert(p); err != nil {
		log.Printf("upsert player: %v", err)
		s.renderError(w, "无法保存排行榜数据", onlineID)
		return
	}

	// Phase 1: 成功后立即更新内存 + Top1000
	s.leaderboard.Upsert(p)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    url.QueryEscape(summary.DisplayID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAge,
	})

	targetPage := s.findPlayerPage(summary.OnlineID)
	redirectURL := fmt.Sprintf("/?page=%d&highlight=%s", targetPage, url.QueryEscape(summary.OnlineID))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	onlineID := strings.TrimSpace(r.FormValue("online_id"))

	if onlineID == "" {
		s.renderError(w, "请输入 PSN Online ID", "")
		return
	}

	if err := validateOnlineID(onlineID); err != nil {
		s.renderError(w, err.Error(), onlineID)
		return
	}

	// Check if player is on the leaderboard
	existing, err := s.store.Get(onlineID)
	if err != nil {
		log.Printf("check existing player: %v", err)
		s.renderError(w, "查找失败", onlineID)
		return
	}
	if existing == nil {
		s.renderError(w, "该玩家尚未入榜", onlineID)
		return
	}

	// Check cooldown (15 minutes)
	if time.Since(existing.SyncedAt) < 15*time.Minute {
		s.renderError(w, "同步过于频繁", onlineID)
		return
	}

	// Perform PSN lookup
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	summary, err := s.source.Lookup(ctx, onlineID)
	if err != nil {
		msg := mapTrophyError(err)
		s.renderError(w, msg, onlineID)
		return
	}

	// Reject negative trophy counts
	if summary.Bronze < 0 || summary.Silver < 0 || summary.Gold < 0 || summary.Platinum < 0 {
		s.renderError(w, "暂时无法同步奖杯，请稍后重试", onlineID)
		return
	}

	// Validate avatar URL: accept https, or http from Sony CDN
	avatarURL := summary.AvatarURL
	if avatarURL != "" {
		if strings.HasPrefix(avatarURL, "http://") {
			// Upgrade Sony CDN URLs from http to https
			if strings.Contains(avatarURL, "static-resource.np.community.playstation.net") {
				avatarURL = strings.Replace(avatarURL, "http://", "https://", 1)
			} else {
				// Reject other http URLs
				avatarURL = ""
			}
		} else if !strings.HasPrefix(avatarURL, "https://") {
			// Reject non-http(s) schemes
			avatarURL = ""
		}
	}

	// Upsert player (preserves joined_at)
	p := player.Player{
		OnlineID:  summary.OnlineID,
		DisplayID: summary.DisplayID,
		AvatarURL: avatarURL,
		Bronze:    summary.Bronze,
		Silver:    summary.Silver,
		Gold:      summary.Gold,
		Platinum:  summary.Platinum,
		Score:     rank.Score(summary.Bronze, summary.Silver, summary.Gold, summary.Platinum),
		SyncedAt:  time.Now(),
	}

	// Phase 1: 同步 Upsert SQLite
	if err := s.store.Upsert(p); err != nil {
		log.Printf("upsert player: %v", err)
		s.renderError(w, "无法保存排行榜数据", onlineID)
		return
	}

	// Phase 1: 从 store 读取完整数据（含 JoinedAt）再更新内存
	stored, err := s.store.Get(summary.OnlineID)
	if err != nil {
		log.Printf("get player after upsert: %v", err)
		s.leaderboard.Upsert(p)
	} else if stored != nil {
		s.leaderboard.Upsert(*stored)
	} else {
		s.leaderboard.Upsert(p)
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    url.QueryEscape(summary.DisplayID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAge,
	})

	// Get current page from form data
	page, _ := strconv.Atoi(r.FormValue("page"))
	if page < 1 {
		page = 1
	}

	// Redirect back to the same page with highlight
	redirectURL := fmt.Sprintf("/?page=%d&highlight=%s", page, url.QueryEscape(summary.OnlineID))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	onlineID := strings.TrimSpace(r.URL.Query().Get("online_id"))

	if onlineID == "" {
		s.renderError(w, "请输入 PSN Online ID", "")
		return
	}

	if err := validateOnlineID(onlineID); err != nil {
		s.renderError(w, err.Error(), onlineID)
		return
	}

	// Phase 1: 从内存查找
	p := s.leaderboard.Get(onlineID)
	if p == nil {
		s.renderError(w, "该玩家尚未入榜", onlineID)
		return
	}

	// Set cookie so "我的排名" works after search (US-007)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    url.QueryEscape(p.DisplayID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAge,
	})

	targetPage := s.findPlayerPage(p.OnlineID)
	redirectURL := fmt.Sprintf("/?page=%d&highlight=%s", targetPage, url.QueryEscape(p.OnlineID))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		s.renderError(w, "先入榜或先查找", "")
		return
	}

	onlineID, _ := url.QueryUnescape(cookie.Value)
	onlineID = strings.TrimSpace(onlineID)

	if onlineID == "" {
		s.renderError(w, "先入榜或先查找", "")
		return
	}

	// Phase 1: 从内存查找
	p := s.leaderboard.Get(onlineID)
	if p == nil {
		s.renderError(w, "该玩家尚未入榜", "")
		return
	}

	targetPage := s.findPlayerPage(p.OnlineID)
	redirectURL := fmt.Sprintf("/?page=%d&highlight=%s", targetPage, url.QueryEscape(p.OnlineID))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) findPlayerPage(onlineID string) int {
	// Phase 1: 从内存查找玩家并计算页码
	p := s.leaderboard.Get(onlineID)
	if p == nil {
		return 1
	}
	// rank 是 1-based，页码也是 1-based
	// 第 1-50 名在第 1 页，第 51-100 名在第 2 页
	return ((p.Rank - 1) / pageSize) + 1
}

func validateOnlineID(id string) error {
	if len(id) < minIDLength || len(id) > maxIDLength {
		return fmt.Errorf("PSN Online ID 须为 3–16 位字母、数字、连字符或下划线")
	}
	if !idPattern.MatchString(id) {
		return fmt.Errorf("PSN Online ID 须为 3–16 位字母、数字、连字符或下划线")
	}
	return nil
}

func mapTrophyError(err error) string {
	if tErr, ok := err.(*trophy.Error); ok {
		switch tErr.Kind {
		case trophy.KindNotFound:
			return "找不到该 PSN 用户"
		case trophy.KindPrivate:
			return "该用户奖杯未公开，无法入榜"
		case trophy.KindNoCredentials:
			return "服务端未配置 PSN 凭证"
		case trophy.KindInvalidCredentials:
			return "PSN 凭证无效，请重新获取 NPSSO"
		case trophy.KindUpstream:
			return "暂时无法同步奖杯，请稍后重试"
		case trophy.KindCooldown:
			return "同步过于频繁"
		}
	}
	return "暂时无法同步奖杯，请稍后重试"
}

func (s *Server) render(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.ExecuteTemplate(w, "leaderboard.html", data); err != nil {
		log.Printf("render template: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *Server) renderError(w http.ResponseWriter, errMsg, inputID string) {
	// Phase 1: 从内存读取第一页，用于错误页面仍显示榜单
	var views []rankedPlayerView
	var currentPage, totalPages int
	
	count := s.leaderboard.Count()
	if count > 0 {
		totalPages = s.leaderboard.TotalPages(pageSize)
		paged := s.leaderboard.Page(1, pageSize)
		views = make([]rankedPlayerView, len(paged))
		for i, p := range paged {
			views[i] = rankedPlayerView{RankedPlayer: p}
		}
		currentPage = 1
	}

	s.render(w, pageData{
		Players:     views,
		CurrentPage: currentPage,
		TotalPages:  totalPages,
		Error:       errMsg,
		InputID:     inputID,
	})
}

const (
	maxSeedCount  = 1000
	maxSeedRetry  = 10
	seedIDPrefix  = "sim"
	seedIDMaxNum  = 10000000 // 7 digits: sim + 7 digits = 10 chars, well under 16
)

// handleAdminSeed seeds the database with simulated users.
// Only accepts requests from loopback addresses (127.0.0.1, ::1).
func (s *Server) handleAdminSeed(w http.ResponseWriter, r *http.Request) {
	// Check remote address is loopback
	remoteAddr := r.RemoteAddr
	if !isLoopback(remoteAddr) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "access denied: seed endpoint only accessible from localhost",
		})
		return
	}

	countStr := r.FormValue("count")
	if countStr == "" {
		countStr = r.URL.Query().Get("count")
	}

	count, err := strconv.Atoi(countStr)
	if err != nil || count <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "invalid count parameter, must be positive integer",
		})
		return
	}

	if count > maxSeedCount {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": fmt.Sprintf("count exceeds maximum allowed (%d)", maxSeedCount),
		})
		return
	}

	inserted := 0
	failed := 0

	for i := 0; i < count; i++ {
		var onlineID string
		retries := 0
		
		// Generate unique online_id with retries
		// Format: "sim" + 7-digit number (total 10 chars, well under 16-char limit)
		for {
			randomNum := rand.Intn(seedIDMaxNum)
			onlineID = fmt.Sprintf("%s%07d", seedIDPrefix, randomNum)
			
			// Validate length (must be 3-16 chars)
			if len(onlineID) < minIDLength || len(onlineID) > maxIDLength {
				log.Printf("generated ID length invalid: %d", len(onlineID))
				failed++
				break
			}
			
			// Check if already exists
			existing, err := s.store.Get(onlineID)
			if err != nil {
				log.Printf("check existing user: %v", err)
			}
			
			if existing == nil {
				break // unique ID found
			}
			
			retries++
			if retries >= maxSeedRetry {
				log.Printf("failed to generate unique ID after %d retries", maxSeedRetry)
				failed++
				break
			}
		}

		if retries >= maxSeedRetry {
			continue // skip this user
		}

		// Generate random trophy counts with reasonable limits
		bronze := rand.Intn(5001)    // 0-5000
		silver := rand.Intn(2001)    // 0-2000
		gold := rand.Intn(801)       // 0-800
		platinum := rand.Intn(201)   // 0-200

		// Random avatar letter A-Z
		avatarLetter := string(rune('A' + rand.Intn(26)))

		p := player.Player{
			OnlineID:  onlineID,
			DisplayID: onlineID,
			AvatarURL: "letter:" + avatarLetter,
			Bronze:    bronze,
			Silver:    silver,
			Gold:      gold,
			Platinum:  platinum,
			Score:     rank.Score(bronze, silver, gold, platinum),
			SyncedAt:  time.Now(),
		}

		// Phase 1: 同步 Upsert SQLite
		if err := s.store.Upsert(p); err != nil {
			log.Printf("seed upsert: %v", err)
			failed++
			continue
		}

		// Phase 1: 从 store 读取完整数据（含 JoinedAt）再更新内存
		stored, err := s.store.Get(onlineID)
		if err != nil {
			log.Printf("get player after seed upsert: %v", err)
			s.leaderboard.Upsert(p)
		} else if stored != nil {
			s.leaderboard.Upsert(*stored)
		} else {
			s.leaderboard.Upsert(p)
		}

		inserted++
	}

	// If we couldn't insert any users when requested, return error
	if inserted == 0 && count > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "failed to insert any users",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"inserted": inserted,
		"failed":   failed,
	})
}

// isLoopback checks if the remote address is a loopback address.
func isLoopback(remoteAddr string) bool {
	// remoteAddr format: "ip:port" or "[ipv6]:port"
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If no port, try as-is
		host = remoteAddr
	}

	// Remove brackets from IPv6
	host = strings.Trim(host, "[]")

	// Check common loopback addresses
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return true
	}

	// Check if it's in 127.0.0.0/8 range
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return true
	}

	return false
}

// ReloadFromStore 从 store 重新加载全量数据到内存（测试辅助方法）。
func (s *Server) ReloadFromStore() error {
	players, err := s.store.ListAll()
	if err != nil {
		return err
	}
	s.leaderboard.Load(players)
	return nil
}
