package psn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func npssoFromDotEnv(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "PSN_NPSSO" {
			continue
		}
		value = strings.TrimSpace(value)
		return strings.Trim(value, `"'`)
	}
	return ""
}

// TestLiveSonyCompleteJSON hits Sony when PSN_LIVE=1.
// Required env: PSN_NPSSO. Optional: PSN_ONLINE_ID (default cutecleverdevil).
// Test output logs the complete JSON bodies (no tokens).
func TestLiveSonyCompleteJSON(t *testing.T) {
	if os.Getenv("PSN_LIVE") != "1" {
		t.Skip("set PSN_LIVE=1 and PSN_NPSSO to fetch complete Sony JSON")
	}
	npsso := strings.TrimSpace(os.Getenv("PSN_NPSSO"))
	if npsso == "" {
		npsso = npssoFromDotEnv(filepath.Join("..", "..", ".env"))
	}
	if npsso == "" {
		t.Fatal("PSN_LIVE=1 requires PSN_NPSSO env or repo-root .env")
	}
	onlineID := strings.TrimSpace(os.Getenv("PSN_ONLINE_ID"))
	if onlineID == "" {
		onlineID = "cutecleverdevil"
	}

	c := New(npsso)
	c.EnableDump()
	sum, err := c.Lookup(context.Background(), onlineID)
	if err != nil {
		t.Fatalf("Lookup(%s): %v", onlineID, err)
	}
	if len(sum.TrophyJSON) == 0 {
		t.Fatal("TrophyJSON empty")
	}
	assertCompleteTrophyJSON(t, sum.TrophyJSON)
	t.Logf("summary DisplayID=%s platinum=%d gold=%d silver=%d bronze=%d score=%d",
		sum.DisplayID, sum.Platinum, sum.Gold, sum.Silver, sum.Bronze, sum.Score)
	t.Logf("complete trophySummary JSON:\n%s", prettyTestJSON(sum.TrophyJSON))

	dumps := c.Dumps()
	if len(dumps) == 0 {
		t.Fatal("no dumped Sony responses")
	}
	for _, d := range dumps {
		if strings.Contains(d.URL, "/oauth") || strings.Contains(d.URL, "/token") || strings.Contains(d.URL, "/authorize") {
			t.Fatalf("oauth dumped: %s", d.URL)
		}
		text := string(d.Body)
		if strings.Contains(text, npsso) {
			t.Fatal("dump leaked NPSSO")
		}
		t.Logf("=== %s %s (%d) ===\n%s", d.Method, d.URL, d.Status, prettyTestJSON(d.Body))
		if strings.Contains(d.URL, "/trophySummary") {
			assertCompleteTrophyJSON(t, d.Body)
		}
	}
}

func prettyTestJSON(body []byte) string {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return string(body)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(body)
	}
	return string(out)
}
