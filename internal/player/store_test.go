package player

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreUpsertAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	p1 := Player{
		OnlineID:  "testuser",
		DisplayID: "TestUser",
		AvatarURL: "https://example.com/avatar.jpg",
		Bronze:    100,
		Silver:    50,
		Gold:      25,
		Platinum:  5,
		Score:     5000,
		JoinedAt:  time.Now().UTC(),
		SyncedAt:  time.Now().UTC(),
	}

	if err := store.Upsert(p1); err != nil {
		t.Fatalf("upsert player: %v", err)
	}

	got, err := store.Get("testuser")
	if err != nil {
		t.Fatalf("get player: %v", err)
	}
	if got == nil {
		t.Fatal("expected player, got nil")
	}
	if got.DisplayID != "TestUser" {
		t.Errorf("DisplayID = %q, want %q", got.DisplayID, "TestUser")
	}
	if got.Score != 5000 {
		t.Errorf("Score = %d, want %d", got.Score, 5000)
	}

	originalJoinedAt := got.JoinedAt

	time.Sleep(10 * time.Millisecond)

	p1.Bronze = 200
	p1.Score = 6000
	if err := store.Upsert(p1); err != nil {
		t.Fatalf("upsert update: %v", err)
	}

	updated, err := store.Get("testuser")
	if err != nil {
		t.Fatalf("get updated player: %v", err)
	}
	if updated.Bronze != 200 {
		t.Errorf("Bronze = %d, want %d", updated.Bronze, 200)
	}
	if updated.Score != 6000 {
		t.Errorf("Score = %d, want %d", updated.Score, 6000)
	}
	if !updated.JoinedAt.Equal(originalJoinedAt) {
		t.Errorf("JoinedAt changed from %v to %v", originalJoinedAt, updated.JoinedAt)
	}
	if updated.SyncedAt.Before(originalJoinedAt) {
		t.Errorf("SyncedAt %v should not be before JoinedAt %v", updated.SyncedAt, originalJoinedAt)
	}
}

func TestStoreCaseInsensitive(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	p := Player{
		OnlineID:  "MixedCase",
		DisplayID: "MixedCase",
		Score:     100,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := store.Get("mixedcase")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected player, got nil")
	}
	if got.DisplayID != "MixedCase" {
		t.Errorf("DisplayID = %q, want %q", got.DisplayID, "MixedCase")
	}
}

func TestStoreListAll(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	players := []Player{
		{OnlineID: "player1", DisplayID: "Player1", Score: 100},
		{OnlineID: "player2", DisplayID: "Player2", Score: 200},
		{OnlineID: "player3", DisplayID: "Player3", Score: 300},
	}

	for _, p := range players {
		if err := store.Upsert(p); err != nil {
			t.Fatalf("upsert %s: %v", p.OnlineID, err)
		}
	}

	got, err := store.ListAll()
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d players, want 3", len(got))
	}
}

func TestStoreRestart(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	p := Player{
		OnlineID:  "persistent",
		DisplayID: "Persistent",
		Score:     999,
		Bronze:    10,
	}
	if err := store.Upsert(p); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	store.Close()

	store2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store2.Close()

	got, err := store2.Get("persistent")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if got == nil {
		t.Fatal("player not found after restart")
	}
	if got.Score != 999 {
		t.Errorf("Score = %d, want 999", got.Score)
	}
	if got.Bronze != 10 {
		t.Errorf("Bronze = %d, want 10", got.Bronze)
	}
}

func TestStoreGetNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	got, err := store.Get("nonexistent")
	if err != nil {
		t.Fatalf("get nonexistent: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestStoreOpenInvalidPath(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}

	_, err := Open("/invalid/nonexistent/path/test.db")
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}
