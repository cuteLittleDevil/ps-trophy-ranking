package main

import (
	"errors"
	"strings"
	"testing"

	"ps-trophy-ranking/internal/psn"
)

func TestValidOnlineID(t *testing.T) {
	t.Parallel()
	
	valid := []string{"cutecleverdevil", "123test", "test-user", "user_name", "abc"}
	for _, id := range valid {
		if !validOnlineID(id) {
			t.Errorf("%q should be valid", id)
		}
	}
	
	invalid := []string{"ab", "bad id", "user@name", "17characterslimit", ""}
	for _, id := range invalid {
		if validOnlineID(id) {
			t.Errorf("%q should be invalid", id)
		}
	}
}

func TestUserMessage(t *testing.T) {
	t.Parallel()
	got := userMessage(&psn.Error{Kind: psn.KindNotFound}, true)
	if got != "找不到该 PSN 用户" {
		t.Fatalf("got %q", got)
	}
	if userMessage(errors.New("boom"), true) != "暂时无法同步奖杯，请稍后重试" {
		t.Fatal("unknown errors must map to upstream text")
	}
}

func TestParseArgs(t *testing.T) {
	t.Parallel()
	id, raw, help, err := parseArgs([]string{"cutecleverdevil"})
	if err != nil || raw || help || id != "cutecleverdevil" {
		t.Fatalf("plain: id=%q raw=%v help=%v err=%v", id, raw, help, err)
	}
	id, raw, help, err = parseArgs([]string{"--raw", "cutecleverdevil"})
	if err != nil || !raw || help || id != "cutecleverdevil" {
		t.Fatalf("--raw first: id=%q raw=%v help=%v err=%v", id, raw, help, err)
	}
	id, raw, help, err = parseArgs([]string{"cutecleverdevil", "--json"})
	if err != nil || !raw || id != "cutecleverdevil" {
		t.Fatalf("--json last: id=%q raw=%v err=%v", id, raw, err)
	}
	_, _, help, err = parseArgs([]string{"--help"})
	if err != nil || !help {
		t.Fatalf("help: help=%v err=%v", help, err)
	}
	if _, _, _, err = parseArgs([]string{"--nope", "id"}); err == nil {
		t.Fatal("unknown flag must error")
	}
	if _, _, _, err = parseArgs(nil); err == nil {
		t.Fatal("missing id must error")
	}
}

func TestPrettyJSON(t *testing.T) {
	t.Parallel()
	got := prettyJSON([]byte(`{"earnedTrophies":{"platinum":14,"gold":48}}`))
	if !strings.Contains(got, `"platinum": 14`) || !strings.Contains(got, "earnedTrophies") {
		t.Fatalf("pretty = %s", got)
	}
	if prettyJSON(nil) != "(empty)" {
		t.Fatal("empty body")
	}
	if prettyJSON([]byte("not-json")) != "not-json" {
		t.Fatal("non-json must pass through")
	}
}
