package psn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const secretNPSSO = "npsso-secret-token-do-not-leak-0123456789abcd"

func TestLookupEmptyNPSSODoesNotHitNetwork(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	t.Cleanup(srv.Close)

	c := NewWithEndpoints("", testEndpoints(srv.URL))
	_, err := c.Lookup(context.Background(), "cutecleverdevil")
	if !errors.Is(err, kindErr(KindNoCredentials)) {
		t.Fatalf("err = %v", err)
	}
	if hits != 0 {
		t.Fatalf("network hits = %d", hits)
	}
}

func TestLookupOtherUser(t *testing.T) {
	t.Parallel()
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		search: map[string]searchHit{
			"cutecleverdevil": {accountID: "111", onlineID: "CuteCleverDevil", avatar: "https://img.example/xl.png"},
		},
		trophies: map[string]Counts{
			"111": {Bronze: 10, Silver: 4, Gold: 2, Platinum: 1},
		},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	sum, err := c.Lookup(context.Background(), "cutecleverdevil")
	if err != nil {
		t.Fatal(err)
	}
	if sum.DisplayID != "CuteCleverDevil" {
		t.Fatalf("DisplayID = %q", sum.DisplayID)
	}
	if sum.Counts != (Counts{Bronze: 10, Silver: 4, Gold: 2, Platinum: 1}) {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	if sum.Score != Score(10, 4, 2, 1) {
		t.Fatalf("score = %d", sum.Score)
	}
	if sum.AvatarURL != "https://img.example/xl.png" {
		t.Fatalf("avatar = %q", sum.AvatarURL)
	}
	assertNoSecret(t, err)
}

func TestLookupSelfSkipsSearch(t *testing.T) {
	t.Parallel()
	var searched bool
	srv := newPSNServer(t, psnScript{
		meOnlineID: "CuteCleverDevil",
		onSearch:   func() { searched = true },
		trophies: map[string]Counts{
			"me": {Bronze: 3, Silver: 2, Gold: 1, Platinum: 0},
		},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	sum, err := c.Lookup(context.Background(), "CuteCleverDevil")
	if err != nil {
		t.Fatal(err)
	}
	if searched {
		t.Fatal("search must not run for the authenticated account")
	}
	if sum.Score != Score(3, 2, 1, 0) {
		t.Fatalf("score = %d", sum.Score)
	}
}

func TestLookupContinuesWhenMeProfileRejected(t *testing.T) {
	t.Parallel()
	srv := newPSNServer(t, psnScript{
		meStatus:   http.StatusBadRequest,
		meOnlineID: "operator",
		search: map[string]searchHit{
			"cutecleverdevil": {accountID: "111", onlineID: "CuteCleverDevil", avatar: "https://img.example/xl.png"},
		},
		trophies: map[string]Counts{
			"111": {Bronze: 10, Silver: 4, Gold: 2, Platinum: 1},
		},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	sum, err := c.Lookup(context.Background(), "cutecleverdevil")
	if err != nil {
		t.Fatal(err)
	}
	if sum.DisplayID != "CuteCleverDevil" || sum.Platinum != 1 {
		t.Fatalf("got %+v", sum)
	}
}

func TestLookupNotFound(t *testing.T) {
	t.Parallel()
	srv := newPSNServer(t, psnScript{meOnlineID: "operator"})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	_, err := c.Lookup(context.Background(), "no_such_user")
	if !errors.Is(err, kindErr(KindNotFound)) {
		t.Fatalf("err = %v", err)
	}
	assertNoSecret(t, err)
}

func TestLookupPrivate(t *testing.T) {
	t.Parallel()
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		search: map[string]searchHit{
			"hiddenplayer": {accountID: "222", onlineID: "hiddenplayer"},
		},
		private: map[string]bool{"222": true},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	_, err := c.Lookup(context.Background(), "hiddenplayer")
	if !errors.Is(err, kindErr(KindPrivate)) {
		t.Fatalf("err = %v", err)
	}
	assertNoSecret(t, err)
}

func TestLookupInvalidNPSSO(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/authorize") {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	_, err := c.Lookup(context.Background(), "cutecleverdevil")
	if !errors.Is(err, kindErr(KindInvalidCredentials)) {
		t.Fatalf("err = %v", err)
	}
	assertNoSecret(t, err)
}

func TestLookupLegacyFallback(t *testing.T) {
	t.Parallel()
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		legacy: map[string]searchHit{
			"legacyid": {accountID: "333", onlineID: "LegacyID", avatar: "https://img.example/s.png"},
		},
		trophies: map[string]Counts{
			"333": {Bronze: 1},
		},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	sum, err := c.Lookup(context.Background(), "legacyid")
	if err != nil {
		t.Fatal(err)
	}
	if sum.DisplayID != "LegacyID" || sum.AccountID != "333" {
		t.Fatalf("got %+v", sum)
	}
}

func TestErrorStringOmitsNPSSO(t *testing.T) {
	t.Parallel()
	err := kindErr(KindInvalidCredentials)
	assertNoSecret(t, err)
}

func testEndpoints(origin string) Endpoints {
	return Endpoints{
		Auth:   origin + "/oauth",
		Search: origin + "/search",
		User:   origin + "/users",
		Legacy: origin + "/legacy",
		Trophy: origin + "/trophy",
	}
}

type searchHit struct {
	accountID string
	onlineID  string
	avatar    string
}

type psnScript struct {
	meOnlineID string
	meStatus   int
	search     map[string]searchHit
	legacy     map[string]searchHit
	trophies   map[string]Counts
	private    map[string]bool
	onSearch   func()
}

func newPSNServer(t *testing.T, script psnScript) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "npsso=") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Location", redirectURI+"/?code=v3.testcode")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(tokenResponse{
			AccessToken:  "access-token",
			ExpiresIn:    3600,
			RefreshToken: "refresh-token",
		})
	})
	mux.HandleFunc("/users/me/profiles", func(w http.ResponseWriter, _ *http.Request) {
		if script.meStatus != 0 && script.meStatus != http.StatusOK {
			w.WriteHeader(script.meStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(meProfileJSON{
			OnlineID: script.meOnlineID,
			Avatars: []struct {
				Size string `json:"size"`
				URL  string `json:"url"`
			}{{Size: "xl", URL: "https://img.example/me.png"}},
		})
	})
	mux.HandleFunc("/search/v1/universalSearch", func(w http.ResponseWriter, r *http.Request) {
		if script.onSearch != nil {
			script.onSearch()
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			SearchTerm string `json:"searchTerm"`
		}
		_ = json.Unmarshal(body, &req)
		hit, ok := script.search[strings.ToLower(req.SearchTerm)]
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"domainResponses": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"domainResponses": []any{
				map[string]any{
					"results": []any{
						map[string]any{
							"socialMetadata": map[string]any{
								"accountId": hit.accountID,
								"onlineId":  hit.onlineID,
								"avatarUrl": hit.avatar,
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/legacy/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/legacy/"), "/profile2")
		hit, ok := script.legacy[strings.ToLower(id)]
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 2105356, "message": "User not found"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"profile": map[string]any{
				"onlineId":  hit.onlineID,
				"accountId": hit.accountID,
				"avatarUrls": []any{
					map[string]any{"size": "s", "avatarUrl": hit.avatar},
				},
			},
		})
	})
	mux.HandleFunc("/trophy/v1/users/", func(w http.ResponseWriter, r *http.Request) {
		accountID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/trophy/v1/users/"), "/trophySummary")
		if script.private[accountID] {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		counts, ok := script.trophies[accountID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(trophySummaryResponse{EarnedTrophies: counts})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func assertNoSecret(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), secretNPSSO) {
		t.Fatalf("error leaked NPSSO: %v", err)
	}
}
