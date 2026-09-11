package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/trophy"
)

func TestHomeEmpty(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "还没有玩家入榜") {
		t.Error("expected empty state message")
	}
}

func TestJoinFixture(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	form := url.Values{}
	form.Set("online_id", "fixture_alpha")

	req := httptest.NewRequest("POST", "/join", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "highlight=fixture_alpha") {
		t.Errorf("location = %q, want highlight", location)
	}

	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "online_id" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected online_id cookie")
	}

	p, err := store.Get("fixture_alpha")
	if err != nil {
		t.Fatalf("get player: %v", err)
	}
	if p == nil {
		t.Fatal("player not saved")
	}
	if p.Platinum != 50 {
		t.Errorf("platinum = %d, want 50", p.Platinum)
	}
}

func TestJoinInvalidID(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	tests := []struct {
		name  string
		id    string
		wants string
	}{
		{"empty", "", "请输入 PSN Online ID"},
		{"too short", "ab", "PSN Online ID 须为 3–16 位"},
		{"too long", "abcdefghijklmnopq", "PSN Online ID 须为 3–16 位"},
		{"invalid chars", "test@user", "PSN Online ID 须为 3–16 位"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{}
			form.Set("online_id", tt.id)

			req := httptest.NewRequest("POST", "/join", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
			}

			body := w.Body.String()
			if !strings.Contains(body, tt.wants) {
				t.Errorf("expected error message %q in response", tt.wants)
			}
		})
	}
}

func TestJoinNotFound(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	form := url.Values{}
	form.Set("online_id", "nonexistent")

	req := httptest.NewRequest("POST", "/join", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "找不到该 PSN 用户") {
		t.Error("expected not found error")
	}
}

func TestSearchFound(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	p := player.Player{
		OnlineID:  "testuser",
		DisplayID: "TestUser",
		Score:     100,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest("GET", "/search?online_id=testuser", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "highlight=testuser") {
		t.Errorf("location = %q, want highlight", location)
	}

	// Assert Set-Cookie for "我的排名" (US-007)
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "online_id" && strings.Contains(c.Value, "TestUser") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected online_id cookie to be set on successful search")
	}
}

func TestSearchNotFound(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	req := httptest.NewRequest("GET", "/search?online_id=notinboard", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "该玩家尚未入榜") {
		t.Error("expected not on board message")
	}
}

func TestMeNoCookie(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	req := httptest.NewRequest("GET", "/me", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "先入榜或先查找") {
		t.Error("expected missing cookie message")
	}
}

func TestMeWithCookie(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	p := player.Player{
		OnlineID:  "cookieuser",
		DisplayID: "CookieUser",
		Score:     200,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	req := httptest.NewRequest("GET", "/me", nil)
	req.AddCookie(&http.Cookie{Name: "online_id", Value: "CookieUser"})
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "highlight=cookieuser") {
		t.Errorf("location = %q, want highlight", location)
	}
}

func TestJoinRejectsNegativeCounts(t *testing.T) {
	_, store := setupTestServer(t)
	defer store.Close()

	// Create a test source that returns negative counts
	negSource := &testSource{
		lookups: map[string]*trophy.Summary{
			"baduser": {
				OnlineID:  "baduser",
				DisplayID: "BadUser",
				AvatarURL: "",
				Counts: trophy.Counts{
					Bronze:   -1,
					Silver:   10,
					Gold:     5,
					Platinum: 2,
				},
			},
		},
	}

	templatesFS := os.DirFS("../../web/templates")
	staticFS := os.DirFS("../../web/static")
	server, _ := New(store, negSource, templatesFS, staticFS)

	form := url.Values{}
	form.Set("online_id", "baduser")

	req := httptest.NewRequest("POST", "/join", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "暂时无法同步奖杯，请稍后重试") {
		t.Error("expected upstream error message for negative counts")
	}

	// Verify player was NOT saved
	p, _ := store.Get("baduser")
	if p != nil {
		t.Error("player with negative counts should not be saved")
	}
}

func TestJoinValidatesAvatarURL(t *testing.T) {
	_, _ = setupTestServer(t)

	tests := []struct {
		name      string
		avatarURL string
		wantEmpty bool
	}{
		{"https URL accepted", "https://example.com/avatar.jpg", false},
		{"http URL rejected", "http://example.com/avatar.jpg", true},
		{"ftp URL rejected", "ftp://example.com/avatar.jpg", true},
		{"javascript rejected", "javascript:alert(1)", true},
		{"empty accepted", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")
			tmpStore, err := player.Open(dbPath)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}
			defer tmpStore.Close()

			testSrc := &testSource{
				lookups: map[string]*trophy.Summary{
					"testuser": {
						OnlineID:  "testuser",
						DisplayID: "TestUser",
						AvatarURL: tt.avatarURL,
						Counts:    trophy.Counts{Bronze: 10, Silver: 5, Gold: 2, Platinum: 1},
					},
				},
			}

			templatesFS := os.DirFS("../../web/templates")
			staticFS := os.DirFS("../../web/static")
			srv, _ := New(tmpStore, testSrc, templatesFS, staticFS)

			form := url.Values{}
			form.Set("online_id", "testuser")

			req := httptest.NewRequest("POST", "/join", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusSeeOther {
				t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
			}

			p, _ := tmpStore.Get("testuser")
			if p == nil {
				t.Fatal("player should be saved")
			}

			if tt.wantEmpty && p.AvatarURL != "" {
				t.Errorf("avatarURL = %q, want empty for invalid scheme", p.AvatarURL)
			}
			if !tt.wantEmpty && p.AvatarURL == "" {
				t.Errorf("avatarURL should not be empty for valid https URL")
			}
		})
	}
}

func TestPagination(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	for i := 0; i < 60; i++ {
		idNum := i + 1
		p := player.Player{
			OnlineID:  "player" + string(rune('0'+idNum%10)) + string(rune('a'+(idNum/10))),
			DisplayID: "Player" + string(rune('0'+idNum%10)),
			Score:     1000 - i,
		}
		if err := store.Upsert(p); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}

	req := httptest.NewRequest("GET", "/?page=2", nil)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "第 2 页") {
		t.Logf("Response body:\n%s", body)
		t.Error("expected page 2 indicator")
	}
}

func setupTestServer(t *testing.T) (*Server, *player.Store) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := player.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	// Create test source with fixture-like data
	source := &testSource{
		lookups: map[string]*trophy.Summary{
			"fixture_alpha": {
				OnlineID:  "fixture_alpha",
				DisplayID: "Fixture_Alpha",
				AvatarURL: "",
				Counts: trophy.Counts{
					Bronze:   1000,
					Silver:   500,
					Gold:     200,
					Platinum: 50,
				},
			},
		},
	}

	templatesFS := os.DirFS("../../web/templates")
	staticFS := os.DirFS("../../web/static")

	server, err := New(store, source, templatesFS, staticFS)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	return server, store
}

type testSource struct {
	lookups map[string]*trophy.Summary
	err     error
}

func (ts *testSource) Lookup(ctx context.Context, onlineID string) (*trophy.Summary, error) {
	if ts.err != nil {
		return nil, ts.err
	}
	s, ok := ts.lookups[strings.ToLower(onlineID)]
	if !ok {
		return nil, trophy.NewError(trophy.KindNotFound)
	}
	return s, nil
}

func TestAdminSeedSuccess(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	form := url.Values{}
	form.Set("count", "5")

	req := httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:12345" // Loopback
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !resp["ok"].(bool) {
		t.Error("expected ok=true")
	}

	inserted := int(resp["inserted"].(float64))
	if inserted != 5 {
		t.Errorf("inserted = %d, want 5", inserted)
	}

	// Check IDs are in correct format and length
	players, _ := store.ListAll()
	for _, p := range players {
		if !strings.HasPrefix(p.OnlineID, "sim") {
			t.Errorf("online_id = %s, want prefix 'sim'", p.OnlineID)
		}
		if len(p.OnlineID) < 3 || len(p.OnlineID) > 16 {
			t.Errorf("online_id length = %d, must be 3-16", len(p.OnlineID))
		}
	}
}

func TestAdminSeedInvalidCount(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	tests := []struct {
		name  string
		count string
	}{
		{"zero", "0"},
		{"negative", "-1"},
		{"non-numeric", "abc"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{}
			if tt.count != "" {
				form.Set("count", tt.count)
			}

			req := httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}

			var resp map[string]interface{}
			json.NewDecoder(w.Body).Decode(&resp)
			if resp["ok"] != false {
				t.Error("expected ok=false")
			}
		})
	}
}

func TestAdminSeedExceedsMax(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	form := url.Values{}
	form.Set("count", "1001") // Exceeds maxSeedCount (1000)

	req := httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != false {
		t.Error("expected ok=false")
	}
	if !strings.Contains(resp["error"].(string), "maximum") {
		t.Errorf("error message should mention maximum, got: %s", resp["error"])
	}
}

func TestAdminSeedCumulative(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	// First seed
	form := url.Values{}
	form.Set("count", "3")
	req := httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	players1, _ := store.ListAll()
	count1 := len(players1)

	// Second seed
	form = url.Values{}
	form.Set("count", "2")
	req = httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	players2, _ := store.ListAll()
	count2 := len(players2)

	if count2 != count1+2 {
		t.Errorf("second seed should add to existing, got %d want %d", count2, count1+2)
	}
}

func TestAdminSeedNonLoopback(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	form := url.Values{}
	form.Set("count", "5")

	req := httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "192.168.1.100:54321" // Non-loopback
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (Forbidden)", w.Code, http.StatusForbidden)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != false {
		t.Error("expected ok=false for non-loopback access")
	}
}
