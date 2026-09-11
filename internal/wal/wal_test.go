package wal

import (
	"os"
	"path/filepath"
	"ps-trophy-ranking/internal/player"
	"sync/atomic"
	"testing"
	"time"
)

func TestWALAppend(t *testing.T) {
	dir := t.TempDir()

	m, err := New(dir, 3)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	// 追加一个事件
	p := player.Player{
		OnlineID:  "testUser",
		DisplayID: "TestUser",
		Bronze:    100,
		Silver:    50,
		Gold:      20,
		Platinum:  5,
		Score:     4500,
	}

	if err := m.Append(p); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// 检查至少一个分片文件有内容
	hasContent := false
	for i := 0; i < 3; i++ {
		shardPath := filepath.Join(dir, "shard-"+string(rune('0'+i))+".log")
		info, err := os.Stat(shardPath)
		if err == nil && info.Size() > 0 {
			hasContent = true
			break
		}
	}

	if !hasContent {
		t.Error("Expected at least one shard file with content")
	}
}

func TestWALShardDistribution(t *testing.T) {
	dir := t.TempDir()

	m, err := New(dir, 10)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	// 追加多个不同的用户
	for i := 0; i < 100; i++ {
		p := player.Player{
			OnlineID:  "user" + string(rune('0'+i%10)),
			DisplayID: "User" + string(rune('0'+i%10)),
			Bronze:    i * 10,
			Silver:    i * 5,
			Gold:      i * 2,
			Platinum:  i,
			Score:     i * 100,
		}
		if err := m.Append(p); err != nil {
			t.Fatalf("Append user %d: %v", i, err)
		}
	}

	// 检查至少有多个分片文件有内容（分布合理）
	nonEmptyShards := 0
	for i := 0; i < 10; i++ {
		shardPath := filepath.Join(dir, "shard-"+string(rune('0'+i))+".log")
		info, err := os.Stat(shardPath)
		if err == nil && info.Size() > 0 {
			nonEmptyShards++
		}
	}

	if nonEmptyShards < 2 {
		t.Errorf("Expected at least 2 non-empty shards, got %d", nonEmptyShards)
	}
}

func TestSealAndFlush(t *testing.T) {
	dir := t.TempDir()

	m, err := New(dir, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	// 追加几个事件
	for i := 0; i < 5; i++ {
		p := player.Player{
			OnlineID:  "sim000000" + string(rune('0'+i)),
			DisplayID: "sim000000" + string(rune('0'+i)),
			Bronze:    i * 100,
			Silver:    i * 50,
			Gold:      i * 20,
			Platinum:  i * 5,
			Score:     i * 1000,
		}
		if err := m.Append(p); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// 记录 flush 调用次数
	var flushedCount int32
	var flushedPlayers []player.Player

	flushFunc := func(players []player.Player) error {
		atomic.AddInt32(&flushedCount, 1)
		flushedPlayers = append(flushedPlayers, players...)
		return nil
	}

	// 手动触发封段刷盘
	for i := 0; i < 2; i++ {
		if err := m.sealAndFlush(i, flushFunc); err != nil {
			t.Errorf("sealAndFlush shard %d: %v", i, err)
		}
	}

	// 检查 flush 被调用
	if flushedCount == 0 {
		t.Error("Expected flush to be called at least once")
	}

	// 检查有玩家被刷盘
	if len(flushedPlayers) == 0 {
		t.Error("Expected some players to be flushed")
	}

	// 检查 sealed 文件被删除
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "shard-0.log" && entry.Name() != "shard-1.log" {
			t.Errorf("Unexpected file after flush: %s", entry.Name())
		}
	}
}

func TestDeduplication(t *testing.T) {
	dir := t.TempDir()

	m, err := New(dir, 1)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	// 追加同一个用户多次（模拟更新）
	for i := 0; i < 3; i++ {
		p := player.Player{
			OnlineID:  "duplicateUser",
			DisplayID: "DuplicateUser",
			Bronze:    i * 100, // 每次不同
			Silver:    i * 50,
			Gold:      i * 20,
			Platinum:  i * 5,
			Score:     i * 1000,
		}
		if err := m.Append(p); err != nil {
			t.Fatalf("Append iteration %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // 确保时间戳不同
	}

	var flushedPlayers []player.Player
	flushFunc := func(players []player.Player) error {
		flushedPlayers = append(flushedPlayers, players...)
		return nil
	}

	// 触发封段
	if err := m.sealAndFlush(0, flushFunc); err != nil {
		t.Fatalf("sealAndFlush: %v", err)
	}

	// 检查去重：应该只有 1 个玩家（最新的）
	if len(flushedPlayers) != 1 {
		t.Errorf("Expected 1 deduplicated player, got %d", len(flushedPlayers))
	}

	// 检查是最新的数据（Bronze = 200）
	if len(flushedPlayers) == 1 {
		if flushedPlayers[0].Bronze != 200 {
			t.Errorf("Expected latest Bronze=200, got %d", flushedPlayers[0].Bronze)
		}
	}
}

func TestReplaySealed(t *testing.T) {
	dir := t.TempDir()

	// 第一阶段：追加数据并封段（但不删除 sealed 文件）
	m1, err := New(dir, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < 5; i++ {
		p := player.Player{
			OnlineID:  "replayUser" + string(rune('0'+i)),
			DisplayID: "ReplayUser" + string(rune('0'+i)),
			Bronze:    i * 100,
			Silver:    i * 50,
			Gold:      i * 20,
			Platinum:  i * 5,
			Score:     i * 1000,
		}
		if err := m1.Append(p); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// 手动封段但不刷盘（模拟崩溃前的状态）
	for i := 0; i < 2; i++ {
		shardPath := filepath.Join(dir, "shard-"+string(rune('0'+i))+".log")
		sealedPath := filepath.Join(dir, "shard-"+string(rune('0'+i))+".log.sealed-"+string(rune('0'+i)))
		
		m1.shardMu[i].Lock()
		m1.shardFiles[i].Close()
		os.Rename(shardPath, sealedPath)
		// 创建新的空文件
		f, _ := os.OpenFile(shardPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		m1.shardFiles[i] = f
		m1.shardMu[i].Unlock()
	}

	m1.Close()

	// 第二阶段：重启并重放
	m2, err := New(dir, 2)
	if err != nil {
		t.Fatalf("New after restart: %v", err)
	}
	defer m2.Close()

	var replayedPlayers []player.Player
	flushFunc := func(players []player.Player) error {
		replayedPlayers = append(replayedPlayers, players...)
		return nil
	}

	if err := m2.ReplaySealed(flushFunc); err != nil {
		t.Fatalf("ReplaySealed: %v", err)
	}

	// 检查有玩家被重放
	if len(replayedPlayers) == 0 {
		t.Error("Expected some players to be replayed")
	}

	// 检查 sealed 文件被删除
	entries, _ := os.ReadDir(dir)
	hasSealed := false
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "shard-0.log" && entry.Name() != "shard-1.log" {
			hasSealed = true
			break
		}
	}

	if hasSealed {
		t.Error("Expected sealed files to be removed after replay")
	}
}

func TestEmptyShardNoSeal(t *testing.T) {
	dir := t.TempDir()

	m, err := New(dir, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer m.Close()

	// 不追加任何事件，直接尝试封段
	flushFunc := func(players []player.Player) error {
		t.Error("Flush should not be called for empty shard")
		return nil
	}

	if err := m.sealAndFlush(0, flushFunc); err != nil {
		t.Errorf("sealAndFlush on empty shard should not error: %v", err)
	}

	// 检查没有 sealed 文件生成
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() != "shard-0.log" && entry.Name() != "shard-1.log" {
			t.Errorf("Unexpected file for empty shard: %s", entry.Name())
		}
	}
}
