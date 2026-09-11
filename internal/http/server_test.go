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
	"time"

	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/trophy"
	"ps-trophy-ranking/internal/wal"
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
	// Phase 1: 测试中直接写 store 后需要重新加载内存
	if err := server.ReloadFromStore(); err != nil {
		t.Fatalf("reload: %v", err)
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
	// Phase 1: 测试中直接写 store 后需要重新加载内存
	if err := server.ReloadFromStore(); err != nil {
		t.Fatalf("reload: %v", err)
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

	tmpDir := t.TempDir()

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
	walMgr, _ := wal.New(tmpDir, 10)
	t.Cleanup(func() { walMgr.Close() })
	server, _ := New(store, negSource, walMgr, templatesFS, staticFS)

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
			walMgr, _ := wal.New(tmpDir, 10)
			defer walMgr.Close()
			srv, _ := New(tmpStore, testSrc, walMgr, templatesFS, staticFS)

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

	// Phase 1: 测试中直接写 store 后需要重新加载内存
	if err := server.ReloadFromStore(); err != nil {
		t.Fatalf("reload: %v", err)
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
	walDir := filepath.Join(tmpDir, "wal")
	walMgr, _ := wal.New(walDir, 10)
	t.Cleanup(func() { walMgr.Close() })

	server, err := New(store, source, walMgr, templatesFS, staticFS)
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

	// Phase 2: 返回 enqueued 而非 inserted
	enqueued := int(resp["enqueued"].(float64))
	if enqueued != 5 {
		t.Errorf("enqueued = %d, want 5", enqueued)
	}
        // Phase 2: WAL 写入是异步的，不立即验证数据库内容
        // 封段刷盘后数据才会出现在 SQLite，测试只验证入队成功
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

	// Test exceeding the 1,000,000 hard limit
	form := url.Values{}
	form.Set("count", "1000001") // 超过硬顶

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
		t.Error("expected ok=false for count exceeding limit")
	}
	
	// 验证错误消息包含限制值
	if errMsg, ok := resp["error"].(string); ok {
		if !strings.Contains(errMsg, "1000000") {
			t.Errorf("error message should mention limit: %s", errMsg)
		}
	} else {
		t.Error("expected error message")
	}

	// Test exactly at the limit (should succeed)
	form2 := url.Values{}
	form2.Set("count", "1000000")

	req2 := httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.RemoteAddr = "127.0.0.1:12345"
	w2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("status for exactly limit = %d, want %d", w2.Code, http.StatusOK)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	if !resp2["ok"].(bool) {
		t.Error("expected ok=true for count exactly at limit")
	}
}

func TestAdminSeedCumulative(t *testing.T) {
	// Phase 2: WAL 写入是异步的，测试只验证两次入队都成功
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

	var resp1 map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp1)

	if !resp1["ok"].(bool) {
		t.Error("first seed should succeed")
	}

	enqueued1 := int(resp1["enqueued"].(float64))
	if enqueued1 != 3 {
		t.Errorf("first seed enqueued = %d, want 3", enqueued1)
	}

	// Second seed
	form = url.Values{}
	form.Set("count", "2")
	req = httptest.NewRequest("POST", "/admin/seed", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	var resp2 map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp2)

	if !resp2["ok"].(bool) {
		t.Error("second seed should succeed")
	}

	enqueued2 := int(resp2["enqueued"].(float64))
	if enqueued2 != 2 {
		t.Errorf("second seed enqueued = %d, want 2", enqueued2)
	}

	// Phase 2: 不验证累加数据库内容，因为 WAL 刷盘是异步的
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

func TestRefreshSuccess(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := player.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Setup: Insert a player first using Upsert (it will set joined_at and synced_at automatically)
	now := time.Now()
	p := player.Player{
		OnlineID:  "testuser",
		DisplayID: "TestUser",
		Bronze:    100,
		Silver:    50,
		Gold:      20,
		Platinum:  5,
		Score:     4000,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("setup upsert: %v", err)
	}

	// Update synced_at to be past cooldown (20 minutes ago)
	if err := store.UpdateSyncedAt("testuser", now.Add(-20*time.Minute)); err != nil {
		t.Fatalf("update synced_at: %v", err)
	}

	// Create test source with updated trophy counts
	testSrc := &testSource{
		lookups: map[string]*trophy.Summary{
			"testuser": {
				OnlineID:  "testuser",
				DisplayID: "TestUser",
				AvatarURL: "",
				Counts: trophy.Counts{
					Bronze:   150,
					Silver:   60,
					Gold:     25,
					Platinum: 6,
				},
			},
		},
	}

	templatesFS := os.DirFS("../../web/templates")
	staticFS := os.DirFS("../../web/static")
	walMgr, _ := wal.New(tmpDir, 10)
	t.Cleanup(func() { walMgr.Close() })
	server, _ := New(store, testSrc, walMgr, templatesFS, staticFS)

	form := url.Values{}
	form.Set("online_id", "testuser")

	req := httptest.NewRequest("POST", "/refresh", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
		t.Logf("Response body: %s", w.Body.String())
		t.FailNow()
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "highlight=testuser") {
		t.Errorf("location = %q, want highlight", location)
	}

	// Verify player was updated
	updated, _ := store.Get("testuser")
	if updated == nil {
		t.Fatal("player should exist")
	}
	if updated.Bronze != 150 {
		t.Errorf("bronze = %d, want 150", updated.Bronze)
	}
	if updated.Platinum != 6 {
		t.Errorf("platinum = %d, want 6", updated.Platinum)
	}
}

func TestRefreshNotOnBoard(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	form := url.Values{}
	form.Set("online_id", "notinboard")

	req := httptest.NewRequest("POST", "/refresh", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func TestRefreshCooldown(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	// Setup: Insert a player with recent sync
	p := player.Player{
		OnlineID:  "cooldownuser",
		DisplayID: "CooldownUser",
		Bronze:    100,
		Silver:    50,
		Gold:      20,
		Platinum:  5,
		Score:     4000,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("setup upsert: %v", err)
	}

	form := url.Values{}
	form.Set("online_id", "cooldownuser")

	req := httptest.NewRequest("POST", "/refresh", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "同步过于频繁") {
		t.Error("expected cooldown message")
	}
}

func TestRefreshInvalidID(t *testing.T) {
	server, store := setupTestServer(t)
	defer store.Close()

	tests := []struct {
		name  string
		id    string
		wants string
	}{
		{"empty", "", "请输入 PSN Online ID"},
		{"too short", "ab", "PSN Online ID 须为 3–16 位"},
		{"invalid chars", "test@user", "PSN Online ID 须为 3–16 位"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{}
			form.Set("online_id", tt.id)

			req := httptest.NewRequest("POST", "/refresh", strings.NewReader(form.Encode()))
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

func TestRefreshPreservesJoinedAt(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := player.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Setup: Insert a player first
	p := player.Player{
		OnlineID:  "olduser",
		DisplayID: "OldUser",
		Bronze:    100,
		Silver:    50,
		Gold:      20,
		Platinum:  5,
		Score:     4000,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("setup upsert: %v", err)
	}

	// Get the player to capture the original joined_at
	original, err := store.Get("olduser")
	if err != nil || original == nil {
		t.Fatalf("get original player: %v", err)
	}
	originalJoinedAt := original.JoinedAt

	// Update synced_at to be past cooldown (20 minutes ago)
	now := time.Now()
	if err := store.UpdateSyncedAt("olduser", now.Add(-20*time.Minute)); err != nil {
		t.Fatalf("update synced_at: %v", err)
	}

	// Create test source with updated trophy counts
	testSrc := &testSource{
		lookups: map[string]*trophy.Summary{
			"olduser": {
				OnlineID:  "olduser",
				DisplayID: "OldUser",
				AvatarURL: "",
				Counts: trophy.Counts{
					Bronze:   200,
					Silver:   100,
					Gold:     40,
					Platinum: 10,
				},
			},
		},
	}

	templatesFS := os.DirFS("../../web/templates")
	staticFS := os.DirFS("../../web/static")
	walMgr, _ := wal.New(tmpDir, 10)
	t.Cleanup(func() { walMgr.Close() })
	server, _ := New(store, testSrc, walMgr, templatesFS, staticFS)

	form := url.Values{}
	form.Set("online_id", "olduser")

	req := httptest.NewRequest("POST", "/refresh", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	// Verify joined_at was preserved
	updated, _ := store.Get("olduser")
	if updated == nil {
		t.Fatal("player should exist")
	}

	// Check that joined_at is exactly the same (should be preserved by Upsert)
	if !updated.JoinedAt.Equal(originalJoinedAt) {
		t.Errorf("joined_at changed: original=%v, updated=%v", originalJoinedAt, updated.JoinedAt)
	}

	// Check that trophy counts were updated
	if updated.Bronze != 200 {
		t.Errorf("bronze = %d, want 200", updated.Bronze)
	}
}

func TestRefreshWithPage(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := player.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Setup: Insert a player first
	now := time.Now()
	p := player.Player{
		OnlineID:  "pageuser",
		DisplayID: "PageUser",
		Bronze:    100,
		Silver:    50,
		Gold:      20,
		Platinum:  5,
		Score:     4000,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("setup upsert: %v", err)
	}

	// Update synced_at to be past cooldown
	if err := store.UpdateSyncedAt("pageuser", now.Add(-20*time.Minute)); err != nil {
		t.Fatalf("update synced_at: %v", err)
	}

	// Create test source with updated trophy counts
	testSrc := &testSource{
		lookups: map[string]*trophy.Summary{
			"pageuser": {
				OnlineID:  "pageuser",
				DisplayID: "PageUser",
				AvatarURL: "",
				Counts: trophy.Counts{
					Bronze:   150,
					Silver:   60,
					Gold:     25,
					Platinum: 6,
				},
			},
		},
	}

	templatesFS := os.DirFS("../../web/templates")
	staticFS := os.DirFS("../../web/static")
	walMgr, _ := wal.New(tmpDir, 10)
	t.Cleanup(func() { walMgr.Close() })
	server, _ := New(store, testSrc, walMgr, templatesFS, staticFS)

	form := url.Values{}
	form.Set("online_id", "pageuser")
	form.Set("page", "2")

	req := httptest.NewRequest("POST", "/refresh", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("status = %d, want %d", w.Code, http.StatusSeeOther)
	}

	location := w.Header().Get("Location")
	if !strings.Contains(location, "page=2") {
		t.Errorf("location = %q, want page=2", location)
	}
	if !strings.Contains(location, "highlight=pageuser") {
		t.Errorf("location = %q, want highlight", location)
	}
}
