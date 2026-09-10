package main

import (
	"errors"
	"testing"

	"ps-trophy-ranking/internal/psn"
)

func TestValidOnlineID(t *testing.T) {
	t.Parallel()
	if !validOnlineID("cutecleverdevil") {
		t.Fatal("cutecleverdevil must be valid")
	}
	if validOnlineID("ab") || validOnlineID("1abc") || validOnlineID("bad id") {
		t.Fatal("invalid IDs accepted")
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
