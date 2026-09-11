package psn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ps-trophy-ranking/internal/rank"
)

func testdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseCompleteSonyTrophySummaryJSON(t *testing.T) {
	t.Parallel()
	raw := testdata(t, "sony_trophy_summary.json")
	var body trophySummaryResponse
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.AccountID != "0000000000000000000" {
		t.Fatalf("accountId = %q", body.AccountID)
	}
	if body.TrophyLevel != 437 || body.TrophyPoint != 200430 || body.Tier != 5 {
		t.Fatalf("level/point/tier = %d/%d/%d", body.TrophyLevel, body.TrophyPoint, body.Tier)
	}
	want := Counts{Bronze: 6212, Silver: 1450, Gold: 525, Platinum: 55}
	if body.EarnedTrophies != want {
		t.Fatalf("earned = %+v", body.EarnedTrophies)
	}
	local := rank.Score(want.Bronze, want.Silver, want.Gold, want.Platinum)
	if local != body.TrophyPoint {
		t.Fatalf("local score %d != Sony trophyPoint %d", local, body.TrophyPoint)
	}
}

func TestLookupCapturesCompleteSonyJSON(t *testing.T) {
	t.Parallel()
	trophyRaw := testdata(t, "sony_trophy_summary.json")
	searchRaw := testdata(t, "sony_universal_search.json")
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		searchRaw:  map[string][]byte{"cutecleverdevil": searchRaw},
		trophyRaw:  map[string][]byte{"111": trophyRaw},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	c.EnableDump()
	sum, err := c.Lookup(context.Background(), "cutecleverdevil")
	if err != nil {
		t.Fatal(err)
	}
	if sum.DisplayID != "CuteCleverDevil" {
		t.Fatalf("DisplayID = %q", sum.DisplayID)
	}
	if sum.AccountID != "111" {
		t.Fatalf("AccountID = %q", sum.AccountID)
	}
	if sum.Platinum != 55 || sum.Gold != 525 || sum.Silver != 1450 || sum.Bronze != 6212 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	if sum.Score != rank.Score(6212, 1450, 525, 55) {
		t.Fatalf("score = %d", sum.Score)
	}
	if !bytes.Equal(sum.TrophyJSON, trophyRaw) {
		t.Fatalf("TrophyJSON != testdata\ngot:  %s\nwant: %s", sum.TrophyJSON, trophyRaw)
	}
	var dumpedTrophy, dumpedSearch bool
	for _, d := range c.Dumps() {
		if strings.Contains(d.URL, "/oauth") || strings.Contains(d.URL, "/token") {
			t.Fatalf("oauth dumped: %s", d.URL)
		}
		if strings.Contains(string(d.Body), secretNPSSO) || strings.Contains(string(d.Body), "access-token") {
			t.Fatalf("dump leaked secret: %s", d.Body)
		}
		if strings.Contains(d.URL, "/trophySummary") {
			dumpedTrophy = true
			if !bytes.Equal(d.Body, trophyRaw) {
				t.Fatalf("trophy dump != complete Sony JSON\ngot:  %s\nwant: %s", d.Body, trophyRaw)
			}
			if d.Status != http.StatusOK {
				t.Fatalf("status = %d", d.Status)
			}
			assertCompleteTrophyJSON(t, d.Body)
		}
		if strings.Contains(d.URL, "/universalSearch") {
			dumpedSearch = true
			if !bytes.Equal(d.Body, searchRaw) {
				t.Fatalf("search dump != complete Sony JSON\ngot:  %s\nwant: %s", d.Body, searchRaw)
			}
		}
	}
	if !dumpedTrophy || !dumpedSearch {
		t.Fatalf("missing dumps trophy=%v search=%v", dumpedTrophy, dumpedSearch)
	}
}

func TestLookupPicksExactIDFromCompleteSearchJSON(t *testing.T) {
	t.Parallel()
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		searchRaw:  map[string][]byte{"cutecleverdevil": testdata(t, "sony_universal_search.json")},
		trophyRaw:  map[string][]byte{"111": testdata(t, "sony_trophy_summary.json")},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	sum, err := c.Lookup(context.Background(), "CuteCleverDevil")
	if err != nil {
		t.Fatal(err)
	}
	if sum.AccountID != "111" {
		t.Fatalf("must ignore CuteCleverDevilFan, got account %s", sum.AccountID)
	}
}

func TestLookupLegacyCompleteJSON(t *testing.T) {
	t.Parallel()
	raw := testdata(t, "sony_legacy_profile.json")
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		legacyRaw:  map[string][]byte{"legacyid": raw},
		trophies:   map[string]Counts{"333": {Bronze: 1}},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	c.EnableDump()
	sum, err := c.Lookup(context.Background(), "legacyid")
	if err != nil {
		t.Fatal(err)
	}
	if sum.DisplayID != "LegacyID" || sum.AccountID != "333" {
		t.Fatalf("got %+v", sum)
	}
	if sum.AvatarURL != "https://static-resource.np.community.playstation.net/avatar_xl.png" {
		t.Fatalf("avatar = %q", sum.AvatarURL)
	}
	var saw bool
	for _, d := range c.Dumps() {
		if strings.Contains(d.URL, "/profile2") {
			saw = true
			if !bytes.Equal(d.Body, raw) {
				t.Fatalf("legacy dump != complete JSON: %s", d.Body)
			}
		}
	}
	if !saw {
		t.Fatal("legacy profile JSON not dumped")
	}
}

func TestLookupForbiddenCompleteJSON(t *testing.T) {
	t.Parallel()
	raw := testdata(t, "sony_trophy_forbidden.json")
	srv := newPSNServer(t, psnScript{
		meOnlineID: "operator",
		search: map[string]searchHit{
			"hiddenplayer": {accountID: "222", onlineID: "hiddenplayer"},
		},
		private:   map[string]bool{"222": true},
		trophyRaw: map[string][]byte{"222": raw},
	})
	c := NewWithEndpoints(secretNPSSO, testEndpoints(srv.URL))
	c.EnableDump()
	_, err := c.Lookup(context.Background(), "hiddenplayer")
	if !errors.Is(err, kindErr(KindPrivate)) {
		t.Fatalf("err = %v", err)
	}
	var saw bool
	for _, d := range c.Dumps() {
		if strings.Contains(d.URL, "/trophySummary") {
			saw = true
			if !bytes.Equal(d.Body, raw) {
				t.Fatalf("forbidden dump != complete JSON: %s", d.Body)
			}
			if d.Status != http.StatusForbidden {
				t.Fatalf("status = %d", d.Status)
			}
		}
	}
	if !saw {
		t.Fatal("forbidden trophy JSON not dumped")
	}
}

func assertCompleteTrophyJSON(t *testing.T, raw []byte) {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	for _, key := range []string{
		"accountId", "trophyLevel", "trophyPoint", "trophyLevelBasePoint",
		"trophyLevelNextPoint", "progress", "tier", "earnedTrophies",
	} {
		if _, ok := obj[key]; !ok {
			t.Fatalf("complete Sony JSON missing %s: %s", key, raw)
		}
	}
	var earned Counts
	if err := json.Unmarshal(obj["earnedTrophies"], &earned); err != nil {
		t.Fatal(err)
	}
	if earned.Platinum == 0 && earned.Gold == 0 && earned.Silver == 0 && earned.Bronze == 0 {
		t.Fatal("earnedTrophies empty")
	}
}
