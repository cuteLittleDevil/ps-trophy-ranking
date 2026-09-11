package player

import (
	"fmt"
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

// TestUpsertBatch tests batch upsert with multi-VALUES
func TestUpsertBatch(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Test 1: 插入一批新玩家
	players := []Player{
		{OnlineID: "batch1", DisplayID: "Batch1", Score: 100, Bronze: 10},
		{OnlineID: "batch2", DisplayID: "Batch2", Score: 200, Bronze: 20},
		{OnlineID: "batch3", DisplayID: "Batch3", Score: 300, Bronze: 30},
	}

	if err := store.UpsertBatch(players); err != nil {
		t.Fatalf("UpsertBatch insert: %v", err)
	}

	// 验证插入
	for _, p := range players {
		got, err := store.Get(p.OnlineID)
		if err != nil {
			t.Fatalf("get %s: %v", p.OnlineID, err)
		}
		if got == nil {
			t.Fatalf("player %s not found", p.OnlineID)
		}
		if got.Score != p.Score {
			t.Errorf("%s: Score = %d, want %d", p.OnlineID, got.Score, p.Score)
		}
		if got.Bronze != p.Bronze {
			t.Errorf("%s: Bronze = %d, want %d", p.OnlineID, got.Bronze, p.Bronze)
		}
	}

	// Test 2: 更新已有玩家，验证 joined_at 保留
	originalJoinedAt := make(map[string]time.Time)
	for _, p := range players {
		got, _ := store.Get(p.OnlineID)
		originalJoinedAt[p.OnlineID] = got.JoinedAt
	}

	time.Sleep(10 * time.Millisecond)

	// 更新分数
	updatedPlayers := []Player{
		{OnlineID: "batch1", DisplayID: "Batch1Updated", Score: 150, Bronze: 15},
		{OnlineID: "batch2", DisplayID: "Batch2Updated", Score: 250, Bronze: 25},
	}

	if err := store.UpsertBatch(updatedPlayers); err != nil {
		t.Fatalf("UpsertBatch update: %v", err)
	}

	// 验证更新：分数变化，joined_at 保留
	for _, p := range updatedPlayers {
		got, err := store.Get(p.OnlineID)
		if err != nil {
			t.Fatalf("get updated %s: %v", p.OnlineID, err)
		}
		if got.Score != p.Score {
			t.Errorf("%s: Score = %d, want %d", p.OnlineID, got.Score, p.Score)
		}
		if got.Bronze != p.Bronze {
			t.Errorf("%s: Bronze = %d, want %d", p.OnlineID, got.Bronze, p.Bronze)
		}
		if got.DisplayID != p.DisplayID {
			t.Errorf("%s: DisplayID = %q, want %q", p.OnlineID, got.DisplayID, p.DisplayID)
		}
		// joined_at 应该保留
		if !got.JoinedAt.Equal(originalJoinedAt[p.OnlineID]) {
			t.Errorf("%s: JoinedAt changed from %v to %v", p.OnlineID, originalJoinedAt[p.OnlineID], got.JoinedAt)
		}
		// synced_at 应该更新
		if got.SyncedAt.Before(originalJoinedAt[p.OnlineID]) {
			t.Errorf("%s: SyncedAt %v should not be before original JoinedAt %v", p.OnlineID, got.SyncedAt, originalJoinedAt[p.OnlineID])
		}
	}

	// Test 3: 空切片不报错
	if err := store.UpsertBatch([]Player{}); err != nil {
		t.Errorf("UpsertBatch empty slice: %v", err)
	}
}

// TestUpsertBatchLarge tests large batch (>100) to verify chunking
func TestUpsertBatchLarge(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// 插入 250 条记录，验证切块逻辑（每批 100）
	const count = 250
	players := make([]Player, count)
	for i := 0; i < count; i++ {
		players[i] = Player{
			OnlineID:  fmt.Sprintf("large%d", i),
			DisplayID: fmt.Sprintf("Large%d", i),
			Score:     i * 10,
			Bronze:    i,
		}
	}

	if err := store.UpsertBatch(players); err != nil {
		t.Fatalf("UpsertBatch large: %v", err)
	}

	// 验证全部插入成功
	all, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != count {
		t.Errorf("got %d players, want %d", len(all), count)
	}

	// 随机验证几条
	for _, idx := range []int{0, 99, 100, 199, 200, 249} {
		got, err := store.Get(players[idx].OnlineID)
		if err != nil {
			t.Fatalf("get large%d: %v", idx, err)
		}
		if got == nil {
			t.Fatalf("player large%d not found", idx)
		}
		if got.Score != players[idx].Score {
			t.Errorf("large%d: Score = %d, want %d", idx, got.Score, players[idx].Score)
		}
	}
}

// TestUpsertBatchVeryLarge tests very large batch (1000) to stress-test
func TestUpsertBatchVeryLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large batch test in short mode")
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// 插入 1000 条记录
	const count = 1000
	players := make([]Player, count)
	for i := 0; i < count; i++ {
		players[i] = Player{
			OnlineID:  fmt.Sprintf("xlarge%d", i),
			DisplayID: fmt.Sprintf("XLarge%d", i),
			Score:     i * 5,
			Silver:    i % 100,
		}
	}

	if err := store.UpsertBatch(players); err != nil {
		t.Fatalf("UpsertBatch very large: %v", err)
	}

	// 验证总数
	all, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(all) != count {
		t.Errorf("got %d players, want %d", len(all), count)
	}

	// 随机验证
	for _, idx := range []int{0, 500, 999} {
		got, err := store.Get(players[idx].OnlineID)
		if err != nil {
			t.Fatalf("get xlarge%d: %v", idx, err)
		}
		if got == nil {
			t.Fatalf("player xlarge%d not found", idx)
		}
		if got.Score != players[idx].Score {
			t.Errorf("xlarge%d: Score = %d, want %d", idx, got.Score, players[idx].Score)
		}
	}
}

// TestUpsertBatchPreservesExistingJoinedAt tests that UpsertBatch preserves existing joined_at
func TestUpsertBatchPreservesExistingJoinedAt(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// 先用单条 Upsert 插入
	original := Player{
		OnlineID:  "preserve",
		DisplayID: "Preserve",
		Score:     100,
	}
	if err := store.Upsert(original); err != nil {
		t.Fatalf("Upsert original: %v", err)
	}

	got1, _ := store.Get("preserve")
	originalJoinedAt := got1.JoinedAt

	time.Sleep(10 * time.Millisecond)

	// 再用 UpsertBatch 更新
	updated := []Player{
		{OnlineID: "preserve", DisplayID: "PreserveUpdated", Score: 200},
	}
	if err := store.UpsertBatch(updated); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	got2, err := store.Get("preserve")
	if err != nil {
		t.Fatalf("get after batch: %v", err)
	}
	if got2.Score != 200 {
		t.Errorf("Score = %d, want 200", got2.Score)
	}
	if got2.DisplayID != "PreserveUpdated" {
		t.Errorf("DisplayID = %q, want %q", got2.DisplayID, "PreserveUpdated")
	}
	// joined_at 必须保留
	if !got2.JoinedAt.Equal(originalJoinedAt) {
		t.Errorf("JoinedAt changed from %v to %v", originalJoinedAt, got2.JoinedAt)
	}
}
