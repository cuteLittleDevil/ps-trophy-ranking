package rank

import (
	"testing"
)

func TestScore(t *testing.T) {
	tests := []struct {
		name                     string
		bronze, silver, gold, pt int
		want                     int
	}{
		{"all zeros", 0, 0, 0, 0, 0},
		{"only bronze", 10, 0, 0, 0, 150},
		{"only silver", 0, 10, 0, 0, 300},
		{"only gold", 0, 0, 10, 0, 900},
		{"only platinum", 0, 0, 0, 10, 3000},
		{"mixed", 100, 50, 25, 5, 100*15 + 50*30 + 25*90 + 5*300},
		{"real example", 1000, 500, 200, 50, 1000*15 + 500*30 + 200*90 + 50*300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Score(tt.bronze, tt.silver, tt.gold, tt.pt)
			if got != tt.want {
				t.Errorf("Score(%d, %d, %d, %d) = %d, want %d",
					tt.bronze, tt.silver, tt.gold, tt.pt, got, tt.want)
			}
		})
	}
}

func TestScoreAndCompetitionRank(t *testing.T) {
	tests := []struct {
		name    string
		players []Player
		want    []RankedPlayer
	}{
		{
			name: "different scores",
			players: []Player{
				{OnlineID: "low", DisplayID: "Low", Score: 100, Counts: Counts{Bronze: 1}},
				{OnlineID: "high", DisplayID: "High", Score: 300, Counts: Counts{Gold: 1}},
				{OnlineID: "mid", DisplayID: "Mid", Score: 200, Counts: Counts{Silver: 1}},
			},
			want: []RankedPlayer{
				{Player: Player{OnlineID: "high", DisplayID: "High", Score: 300, Counts: Counts{Gold: 1}}, Rank: 1},
				{Player: Player{OnlineID: "mid", DisplayID: "Mid", Score: 200, Counts: Counts{Silver: 1}}, Rank: 2},
				{Player: Player{OnlineID: "low", DisplayID: "Low", Score: 100, Counts: Counts{Bronze: 1}}, Rank: 3},
			},
		},
		{
			name: "same score different platinum - competition rank tie",
			players: []Player{
				{OnlineID: "a", DisplayID: "A", Score: 600, Counts: Counts{Platinum: 2}},
				{OnlineID: "b", DisplayID: "B", Score: 600, Counts: Counts{Platinum: 2}},
				{OnlineID: "c", DisplayID: "C", Score: 600, Counts: Counts{Platinum: 1, Gold: 3}},
			},
			want: []RankedPlayer{
				{Player: Player{OnlineID: "a", DisplayID: "A", Score: 600, Counts: Counts{Platinum: 2}}, Rank: 1},
				{Player: Player{OnlineID: "b", DisplayID: "B", Score: 600, Counts: Counts{Platinum: 2}}, Rank: 1},
				{Player: Player{OnlineID: "c", DisplayID: "C", Score: 600, Counts: Counts{Platinum: 1, Gold: 3}}, Rank: 3},
			},
		},
		{
			name: "same counts - ID tiebreaker",
			players: []Player{
				{OnlineID: "zebra", DisplayID: "Zebra", Score: 100, Counts: Counts{Bronze: 1}},
				{OnlineID: "alpha", DisplayID: "Alpha", Score: 100, Counts: Counts{Bronze: 1}},
			},
			want: []RankedPlayer{
				{Player: Player{OnlineID: "alpha", DisplayID: "Alpha", Score: 100, Counts: Counts{Bronze: 1}}, Rank: 1},
				{Player: Player{OnlineID: "zebra", DisplayID: "Zebra", Score: 100, Counts: Counts{Bronze: 1}}, Rank: 1},
			},
		},
		{
			name: "all zeros",
			players: []Player{
				{OnlineID: "empty1", DisplayID: "Empty1", Score: 0},
				{OnlineID: "empty2", DisplayID: "Empty2", Score: 0},
			},
			want: []RankedPlayer{
				{Player: Player{OnlineID: "empty1", DisplayID: "Empty1", Score: 0}, Rank: 1},
				{Player: Player{OnlineID: "empty2", DisplayID: "Empty2", Score: 0}, Rank: 1},
			},
		},
		{
			name: "same score different gold",
			players: []Player{
				{OnlineID: "golda", DisplayID: "GoldA", Score: 180, Counts: Counts{Gold: 2}},
				{OnlineID: "goldb", DisplayID: "GoldB", Score: 180, Counts: Counts{Gold: 1, Silver: 3}},
			},
			want: []RankedPlayer{
				{Player: Player{OnlineID: "golda", DisplayID: "GoldA", Score: 180, Counts: Counts{Gold: 2}}, Rank: 1},
				{Player: Player{OnlineID: "goldb", DisplayID: "GoldB", Score: 180, Counts: Counts{Gold: 1, Silver: 3}}, Rank: 2},
			},
		},
		{
			name:    "empty list",
			players: []Player{},
			want:    []RankedPlayer{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SortAndNumber(tt.players)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d players, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].OnlineID != tt.want[i].OnlineID {
					t.Errorf("position %d: got ID %q, want %q", i, got[i].OnlineID, tt.want[i].OnlineID)
				}
				if got[i].Rank != tt.want[i].Rank {
					t.Errorf("position %d: got rank %d, want %d", i, got[i].Rank, tt.want[i].Rank)
				}
			}
		})
	}
}

func TestPage(t *testing.T) {
	players := make([]RankedPlayer, 150)
	for i := 0; i < 150; i++ {
		players[i] = RankedPlayer{
			Player: Player{OnlineID: "player" + string(rune('A'+i%26)), Score: 1000 - i},
			Rank:   i + 1,
		}
	}

	tests := []struct {
		name     string
		page     int
		pageSize int
		wantLen  int
		wantRank int
	}{
		{name: "page 1", page: 1, pageSize: 50, wantLen: 50, wantRank: 1},
		{name: "page 2", page: 2, pageSize: 50, wantLen: 50, wantRank: 51},
		{name: "page 3", page: 3, pageSize: 50, wantLen: 50, wantRank: 101},
		{name: "page 4 - beyond", page: 4, pageSize: 50, wantLen: 0, wantRank: 0},
		{name: "page 0 - treated as 1", page: 0, pageSize: 50, wantLen: 50, wantRank: 1},
		{name: "negative page", page: -1, pageSize: 50, wantLen: 50, wantRank: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Page(players, tt.page, tt.pageSize)
			if len(got) != tt.wantLen {
				t.Errorf("got %d items, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen > 0 && got[0].Rank != tt.wantRank {
				t.Errorf("first item rank = %d, want %d", got[0].Rank, tt.wantRank)
			}
		})
	}
}

func TestTotalPages(t *testing.T) {
	tests := []struct {
		name     string
		total    int
		pageSize int
		want     int
	}{
		{name: "exactly 2 pages", total: 100, pageSize: 50, want: 2},
		{name: "partial last page", total: 101, pageSize: 50, want: 3},
		{name: "one page", total: 49, pageSize: 50, want: 1},
		{name: "empty", total: 0, pageSize: 50, want: 0},
		{name: "invalid pageSize", total: 100, pageSize: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TotalPages(tt.total, tt.pageSize)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// TestSortAndNumber_LargeScale 测试大数据量排序性能和正确性。
// 确保使用标准库排序后，O(N log N) 性能可处理 10k-100k 量级。
func TestSortAndNumber_LargeScale(t *testing.T) {
	sizes := []int{1000, 10000, 50000}
	
	for _, size := range sizes {
		t.Run(formatSize(size), func(t *testing.T) {
			players := make([]Player, size)
			for i := 0; i < size; i++ {
				// 创建分数递减的玩家，每 100 个玩家同分以测试竞赛名次
				score := (size - i/100) * 100
				players[i] = Player{
					OnlineID:  formatLargeID(i),
					DisplayID: formatLargeID(i),
					Score:     score,
					Counts: Counts{
						Bronze:   score / 15,
						Silver:   0,
						Gold:     0,
						Platinum: 0,
					},
				}
			}

			// 排序并分配名次
			ranked := SortAndNumber(players)

			// 验证结果长度
			if len(ranked) != size {
				t.Fatalf("expected %d players, got %d", size, len(ranked))
			}

			// 验证排序正确性：检查前后顺序
			for i := 1; i < len(ranked); i++ {
				prev := ranked[i-1]
				curr := ranked[i]
				
				// prev 的分数应 >= curr 的分数
				if prev.Score < curr.Score {
					t.Errorf("position %d: score order violation, prev=%d < curr=%d", i, prev.Score, curr.Score)
				}
				
				// 同分时，检查 online_id 字典序
				if prev.Score == curr.Score && prev.Bronze == curr.Bronze {
					if prev.OnlineID > curr.OnlineID {
						t.Errorf("position %d: ID order violation with same score, prev=%s > curr=%s", i, prev.OnlineID, curr.OnlineID)
					}
				}
			}

			// 验证竞赛名次：第一个玩家应为 Rank 1
			if ranked[0].Rank != 1 {
				t.Errorf("first player rank should be 1, got %d", ranked[0].Rank)
			}

			// 验证同分玩家的竞赛名次相同
			if size >= 200 {
				// 前 100 个玩家应该同分（score = size*100），且 Rank 都为 1
				for i := 0; i < 100; i++ {
					if ranked[i].Rank != 1 {
						t.Errorf("position %d: expected rank 1 (same score group), got %d", i, ranked[i].Rank)
						break
					}
				}
				// 第 101 个玩家应为 Rank 101（竞赛名次跳号）
				if ranked[100].Rank != 101 {
					t.Errorf("position 100: expected rank 101 (competition skip), got %d", ranked[100].Rank)
				}
			}
		})
	}
}

func formatSize(n int) string {
	if n >= 1000 {
		return string(rune('0'+n/1000)) + "k players"
	}
	return string(rune('0'+n/100)) + "00 players"
}

func formatLargeID(i int) string {
	// 生成形如 "player00000", "player00001", ... 的 ID
	return "player" + 
		string(rune('0'+i/10000%10)) +
		string(rune('0'+i/1000%10)) +
		string(rune('0'+i/100%10)) +
		string(rune('0'+i/10%10)) +
		string(rune('0'+i%10))
}
