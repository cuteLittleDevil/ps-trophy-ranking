package memrank

import (
	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/rank"
	"testing"
	"time"
)

func TestLeaderboard_LoadAndGet(t *testing.T) {
	lb := New()
	now := time.Now()

	players := []player.Player{
		{
			OnlineID: "alice", DisplayID: "Alice", AvatarURL: "", Bronze: 100, Silver: 50, Gold: 20, Platinum: 5,
			Score: rank.Score(100, 50, 20, 5), JoinedAt: now, SyncedAt: now,
		},
		{
			OnlineID: "bob", DisplayID: "Bob", AvatarURL: "", Bronze: 200, Silver: 100, Gold: 40, Platinum: 10,
			Score: rank.Score(200, 100, 40, 10), JoinedAt: now, SyncedAt: now,
		},
	}

	lb.Load(players)

	// 检查玩家数
	if count := lb.Count(); count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}

	// 检查门槛分（不足 1000 人应为 -1）
	if threshold := lb.ThresholdScore(); threshold != -1 {
		t.Errorf("expected threshold -1 (less than 1000), got %d", threshold)
	}

	// 查找 Bob（应排第一，分数更高）
	bob := lb.Get("bob")
	if bob == nil {
		t.Fatal("expected bob to be found")
	}
	if bob.Rank != 1 {
		t.Errorf("expected bob rank 1, got %d", bob.Rank)
	}

	// 查找 Alice
	alice := lb.Get("alice")
	if alice == nil {
		t.Fatal("expected alice to be found")
	}
	if alice.Rank != 2 {
		t.Errorf("expected alice rank 2, got %d", alice.Rank)
	}

	// 查找不存在的玩家
	if notFound := lb.Get("charlie"); notFound != nil {
		t.Errorf("expected charlie not found, got %+v", notFound)
	}
}

func TestLeaderboard_Upsert(t *testing.T) {
	lb := New()
	now := time.Now()

	// 初始加载一个玩家
	lb.Load([]player.Player{
		{
			OnlineID: "alice", DisplayID: "Alice", AvatarURL: "", Bronze: 100, Silver: 50, Gold: 20, Platinum: 5,
			Score: rank.Score(100, 50, 20, 5), JoinedAt: now, SyncedAt: now,
		},
	})

	// Upsert 新玩家 Bob
	bob := player.Player{
		OnlineID: "bob", DisplayID: "Bob", AvatarURL: "", Bronze: 200, Silver: 100, Gold: 40, Platinum: 10,
		Score: rank.Score(200, 100, 40, 10), JoinedAt: now, SyncedAt: now,
	}
	lb.Upsert(bob)

	if count := lb.Count(); count != 2 {
		t.Errorf("expected count 2 after upsert, got %d", count)
	}

	// Bob 应排第一
	bobRanked := lb.Get("bob")
	if bobRanked == nil || bobRanked.Rank != 1 {
		t.Errorf("expected bob rank 1, got %+v", bobRanked)
	}

	// 更新 Alice 分数，使其超过 Bob
	aliceUpdated := player.Player{
		OnlineID: "alice", DisplayID: "Alice", AvatarURL: "", Bronze: 300, Silver: 200, Gold: 80, Platinum: 20,
		Score: rank.Score(300, 200, 80, 20), JoinedAt: now, SyncedAt: now,
	}
	lb.Upsert(aliceUpdated)

	if count := lb.Count(); count != 2 {
		t.Errorf("expected count still 2 after update, got %d", count)
	}

	// Alice 应排第一
	aliceRanked := lb.Get("alice")
	if aliceRanked == nil || aliceRanked.Rank != 1 {
		t.Errorf("expected alice rank 1 after update, got %+v", aliceRanked)
	}

	// Bob 应排第二
	bobRanked = lb.Get("bob")
	if bobRanked == nil || bobRanked.Rank != 2 {
		t.Errorf("expected bob rank 2 after alice update, got %+v", bobRanked)
	}
}

func TestLeaderboard_Page(t *testing.T) {
	lb := New()
	now := time.Now()

	// 创建 100 个玩家
	players := make([]player.Player, 100)
	for i := 0; i < 100; i++ {
		score := (100 - i) * 100 // 分数递减：10000, 9900, ..., 100
		players[i] = player.Player{
			OnlineID:  formatID(i),
			DisplayID: formatID(i),
			AvatarURL: "",
			Bronze:    score / 15,
			Silver:    0,
			Gold:      0,
			Platinum:  0,
			Score:     score,
			JoinedAt:  now,
			SyncedAt:  now,
		}
	}
	lb.Load(players)

	// 测试第 1 页，每页 50
	page1 := lb.Page(1, 50)
	if len(page1) != 50 {
		t.Errorf("expected page1 len 50, got %d", len(page1))
	}
	if page1[0].Rank != 1 {
		t.Errorf("expected first player rank 1, got %d", page1[0].Rank)
	}
	if page1[0].Score != 10000 {
		t.Errorf("expected first player score 10000, got %d", page1[0].Score)
	}

	// 测试第 2 页
	page2 := lb.Page(2, 50)
	if len(page2) != 50 {
		t.Errorf("expected page2 len 50, got %d", len(page2))
	}
	if page2[0].Rank != 51 {
		t.Errorf("expected page2 first rank 51, got %d", page2[0].Rank)
	}

	// 测试超出范围的页
	page3 := lb.Page(3, 50)
	if page3 != nil {
		t.Errorf("expected page3 to be nil (out of range), got %d items", len(page3))
	}
}

func TestLeaderboard_Top1000(t *testing.T) {
	lb := New()
	now := time.Now()

	// 创建 1500 个玩家
	players := make([]player.Player, 1500)
	for i := 0; i < 1500; i++ {
		score := (1500 - i) * 100
		players[i] = player.Player{
			OnlineID:  formatID(i),
			DisplayID: formatID(i),
			AvatarURL: "",
			Bronze:    score / 15,
			Silver:    0,
			Gold:      0,
			Platinum:  0,
			Score:     score,
			JoinedAt:  now,
			SyncedAt:  now,
		}
	}
	lb.Load(players)

	// 检查总数
	if count := lb.Count(); count != 1500 {
		t.Errorf("expected count 1500, got %d", count)
	}

	// 检查门槛分（第 1000 名的分数）
	// 排序后：第 1 名 score=150000，第 1000 名 score=50100
	expectedThreshold := 50100
	if threshold := lb.ThresholdScore(); threshold != expectedThreshold {
		t.Errorf("expected threshold %d, got %d", expectedThreshold, threshold)
	}

	// 测试分页：第 1 页（前 50 名）应该从 Top1000 读取
	page1 := lb.Page(1, 50)
	if len(page1) != 50 {
		t.Errorf("expected page1 len 50, got %d", len(page1))
	}
	if page1[0].Score != 150000 {
		t.Errorf("expected first score 150000, got %d", page1[0].Score)
	}

	// 测试分页：第 20 页（名次 951-1000）仍在 Top1000 内
	page20 := lb.Page(20, 50)
	if len(page20) != 50 {
		t.Errorf("expected page20 len 50, got %d", len(page20))
	}

	// 测试分页：第 21 页（名次 1001-1050）超出 Top1000，走全量
	page21 := lb.Page(21, 50)
	if len(page21) != 50 {
		t.Errorf("expected page21 len 50, got %d", len(page21))
	}
	if page21[0].Rank != 1001 {
		t.Errorf("expected page21 first rank 1001, got %d", page21[0].Rank)
	}
}

func TestLeaderboard_TotalPages(t *testing.T) {
	lb := New()
	now := time.Now()

	// 空榜
	if total := lb.TotalPages(50); total != 0 {
		t.Errorf("expected total pages 0 for empty leaderboard, got %d", total)
	}

	// 加载 100 个玩家
	players := make([]player.Player, 100)
	for i := 0; i < 100; i++ {
		players[i] = player.Player{
			OnlineID:  formatID(i),
			DisplayID: formatID(i),
			AvatarURL: "",
			Bronze:    i,
			Silver:    0,
			Gold:      0,
			Platinum:  0,
			Score:     i * 15,
			JoinedAt:  now,
			SyncedAt:  now,
		}
	}
	lb.Load(players)

	// 每页 50，应该有 2 页
	if total := lb.TotalPages(50); total != 2 {
		t.Errorf("expected total pages 2, got %d", total)
	}

	// 每页 30，应该有 4 页（100 / 30 向上取整 = 4）
	if total := lb.TotalPages(30); total != 4 {
		t.Errorf("expected total pages 4, got %d", total)
	}
}

func TestLeaderboard_CompetitionRank(t *testing.T) {
	lb := New()
	now := time.Now()

	// 创建两个完全相同分数的玩家
	players := []player.Player{
		{
			OnlineID: "alice", DisplayID: "Alice", AvatarURL: "", Bronze: 100, Silver: 50, Gold: 20, Platinum: 5,
			Score: rank.Score(100, 50, 20, 5), JoinedAt: now, SyncedAt: now,
		},
		{
			OnlineID: "bob", DisplayID: "Bob", AvatarURL: "", Bronze: 100, Silver: 50, Gold: 20, Platinum: 5,
			Score: rank.Score(100, 50, 20, 5), JoinedAt: now, SyncedAt: now,
		},
		{
			OnlineID: "charlie", DisplayID: "Charlie", AvatarURL: "", Bronze: 50, Silver: 25, Gold: 10, Platinum: 2,
			Score: rank.Score(50, 25, 10, 2), JoinedAt: now, SyncedAt: now,
		},
	}
	lb.Load(players)

	// Alice 和 Bob 应该同分同名次（根据 OnlineID 字典序，alice < bob）
	alice := lb.Get("alice")
	bob := lb.Get("bob")
	charlie := lb.Get("charlie")

	if alice == nil || bob == nil || charlie == nil {
		t.Fatal("expected all players found")
	}

	// Alice 和 Bob 应该并列第 1 名
	if alice.Rank != 1 || bob.Rank != 1 {
		t.Errorf("expected alice and bob both rank 1, got alice=%d bob=%d", alice.Rank, bob.Rank)
	}

	// Charlie 应该是第 3 名（竞赛名次跳号）
	if charlie.Rank != 3 {
		t.Errorf("expected charlie rank 3 (competition skip), got %d", charlie.Rank)
	}
}

func TestLeaderboard_IndexOf(t *testing.T) {
	lb := New()
	now := time.Now()

	// 创建 60 个玩家，前 50 个同分（都是 1000 分）
	players := make([]player.Player, 60)
	for i := 0; i < 60; i++ {
		var score int
		if i < 50 {
			score = 1000 // 前 50 个同分
		} else {
			score = 900 - (i - 50) // 后 10 个分数递减
		}
		players[i] = player.Player{
			OnlineID:  formatID(i),
			DisplayID: formatID(i),
			AvatarURL: "",
			Bronze:    score / 15,
			Silver:    0,
			Gold:      0,
			Platinum:  0,
			Score:     score,
			JoinedAt:  now,
			SyncedAt:  now,
		}
	}
	lb.Load(players)

	// 测试：前 50 个玩家同分，Rank 都是 1，但下标应该是 0-49
	for i := 0; i < 50; i++ {
		id := formatID(i)
		p := lb.Get(id)
		if p == nil {
			t.Fatalf("player %s not found", id)
		}
		if p.Rank != 1 {
			t.Errorf("player %s: expected rank 1, got %d", id, p.Rank)
		}

		index := lb.IndexOf(id)
		if index != i {
			t.Errorf("player %s: expected index %d, got %d", id, i, index)
		}

		// 验证页码计算：下标 0-49 在第 1 页，下标 50-59 在第 2 页
		expectedPage := (index / 50) + 1
		if i < 50 {
			if expectedPage != 1 {
				t.Errorf("player %s (index %d): expected page 1, got %d", id, index, expectedPage)
			}
		}
	}

	// 第 51 个玩家（下标 50）应该在第 2 页
	id50 := formatID(50)
	index50 := lb.IndexOf(id50)
	if index50 != 50 {
		t.Errorf("player %s: expected index 50, got %d", id50, index50)
	}
	page50 := (index50 / 50) + 1
	if page50 != 2 {
		t.Errorf("player %s: expected page 2, got %d", id50, page50)
	}

	// 不存在的玩家应该返回 -1
	if idx := lb.IndexOf("nonexistent"); idx != -1 {
		t.Errorf("nonexistent player: expected index -1, got %d", idx)
	}
}

func formatID(i int) string {
	return "player" + string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
}
