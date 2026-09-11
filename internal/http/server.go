package http

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"net/url"
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
	tmpl        *template.Template
	templatesFS fs.FS
	staticFS    fs.FS
}

// New creates a new HTTP server.
func New(store *player.Store, source trophy.Source, templatesFS, staticFS fs.FS) (*Server, error) {
	funcMap := template.FuncMap{
		"add":        func(a, b int) int { return a + b },
		"sub":        func(a, b int) int { return a - b },
		"gt":         func(a, b int) bool { return a > b },
		"lt":         func(a, b int) bool { return a < b },
		"hasPrefix":  func(s, prefix string) bool { return strings.HasPrefix(s, prefix) },
		"trimPrefix": func(s, prefix string) string { return strings.TrimPrefix(s, prefix) },
	}
	
	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templatesFS, "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		store:       store,
		source:      source,
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

	players, err := s.store.ListAll()
	if err != nil {
		log.Printf("list players: %v", err)
		s.renderError(w, "无法加载排行榜", "")
		return
	}

	if len(players) == 0 {
		s.render(w, pageData{
			EmptyMessage: "还没有玩家入榜",
		})
		return
	}

	rankPlayers := make([]rank.Player, len(players))
	for i, p := range players {
		rankPlayers[i] = rank.Player{
			OnlineID:  p.OnlineID,
			DisplayID: p.DisplayID,
			AvatarURL: p.AvatarURL,
			Counts: rank.Counts{
				Bronze:   p.Bronze,
				Silver:   p.Silver,
				Gold:     p.Gold,
				Platinum: p.Platinum,
			},
			Score: p.Score,
		}
	}

	ranked := rank.SortAndNumber(rankPlayers)
	totalPages := rank.TotalPages(len(ranked), pageSize)

	if page > totalPages && totalPages > 0 {
		s.render(w, pageData{
			EmptyMessage: "没有更多玩家",
			CurrentPage:  page,
			TotalPages:   totalPages,
		})
		return
	}

	paged := rank.Page(ranked, page, pageSize)
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

	// Validate avatar URL: only accept https or empty
	avatarURL := summary.AvatarURL
	if avatarURL != "" && !strings.HasPrefix(avatarURL, "https://") {
		avatarURL = "" // Reject non-https schemes, use placeholder
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

	if err := s.store.Upsert(p); err != nil {
		log.Printf("upsert player: %v", err)
		s.renderError(w, "无法保存排行榜数据", onlineID)
		return
	}

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

	p, err := s.store.Get(onlineID)
	if err != nil {
		log.Printf("get player: %v", err)
		s.renderError(w, "查找失败", onlineID)
		return
	}

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

	p, err := s.store.Get(onlineID)
	if err != nil {
		log.Printf("get player: %v", err)
		s.renderError(w, "查找失败", "")
		return
	}

	if p == nil {
		s.renderError(w, "该玩家尚未入榜", "")
		return
	}

	targetPage := s.findPlayerPage(p.OnlineID)
	redirectURL := fmt.Sprintf("/?page=%d&highlight=%s", targetPage, url.QueryEscape(p.OnlineID))
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (s *Server) findPlayerPage(onlineID string) int {
	players, err := s.store.ListAll()
	if err != nil {
		return 1
	}

	rankPlayers := make([]rank.Player, len(players))
	for i, p := range players {
		rankPlayers[i] = rank.Player{
			OnlineID:  p.OnlineID,
			DisplayID: p.DisplayID,
			AvatarURL: p.AvatarURL,
			Counts: rank.Counts{
				Bronze:   p.Bronze,
				Silver:   p.Silver,
				Gold:     p.Gold,
				Platinum: p.Platinum,
			},
			Score: p.Score,
		}
	}

	ranked := rank.SortAndNumber(rankPlayers)
	for i, p := range ranked {
		if strings.EqualFold(p.OnlineID, onlineID) {
			return (i / pageSize) + 1
		}
	}
	return 1
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
	players, _ := s.store.ListAll()
	
	var views []rankedPlayerView
	var currentPage, totalPages int
	
	if len(players) > 0 {
		rankPlayers := make([]rank.Player, len(players))
		for i, p := range players {
			rankPlayers[i] = rank.Player{
				OnlineID:  p.OnlineID,
				DisplayID: p.DisplayID,
				AvatarURL: p.AvatarURL,
				Counts: rank.Counts{
					Bronze:   p.Bronze,
					Silver:   p.Silver,
					Gold:     p.Gold,
					Platinum: p.Platinum,
				},
				Score: p.Score,
			}
		}
		ranked := rank.SortAndNumber(rankPlayers)
		totalPages = rank.TotalPages(len(ranked), pageSize)
		paged := rank.Page(ranked, 1, pageSize)
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

// handleAdminSeed seeds the database with simulated users.
// No authentication - relies on localhost binding.
func (s *Server) handleAdminSeed(w http.ResponseWriter, r *http.Request) {
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

	inserted := 0
	maxRetries := 10

	for i := 0; i < count; i++ {
		var onlineID string
		retries := 0
		
		// Generate unique online_id with retries
		for {
			randomNum := rand.Intn(1000000)
			onlineID = fmt.Sprintf("simulation%d", randomNum)
			
			// Check if already exists
			existing, err := s.store.Get(onlineID)
			if err != nil {
				log.Printf("check existing user: %v", err)
			}
			
			if existing == nil {
				break // unique ID found
			}
			
			retries++
			if retries >= maxRetries {
				log.Printf("failed to generate unique ID after %d retries", maxRetries)
				break
			}
		}

		if retries >= maxRetries {
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

		if err := s.store.Upsert(p); err != nil {
			log.Printf("seed upsert: %v", err)
			continue
		}

		inserted++
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":       true,
		"inserted": inserted,
	})
}
