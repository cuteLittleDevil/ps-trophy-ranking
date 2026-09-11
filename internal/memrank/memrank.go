package memrank

import (
	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/rank"
	"strings"
	"sync"
)

// Leaderboard 维护全量内存排行榜与 Top1000 视图。
// 线程安全：所有公开方法持有读锁或写锁。
type Leaderboard struct {
	mu sync.RWMutex

	// 全量玩家 map，key 为小写 online_id
	players map[string]*player.Player

	// 有序玩家列表（按竞赛名次排序）
	ranked []rank.RankedPlayer

	// Top1000 视图（从 ranked 切片）
	top1000 []rank.RankedPlayer

	// 门槛分：第 1000 名的 score；不足 1000 人时为 -1
	thresholdScore int
}

// New 创建空的内存排行榜。
func New() *Leaderboard {
	return &Leaderboard{
		players:        make(map[string]*player.Player),
		ranked:         []rank.RankedPlayer{},
		top1000:        []rank.RankedPlayer{},
		thresholdScore: -1, // -∞ 表示所有写入走热路径
	}
}

// Load 从玩家列表加载到内存，并初始化 Top1000 与门槛分。
// 调用方应该在服务启动时调用一次。
func (lb *Leaderboard) Load(players []player.Player) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.players = make(map[string]*player.Player, len(players))
	for i := range players {
		p := &players[i]
		lb.players[strings.ToLower(p.OnlineID)] = p
	}

	lb.rebuildRankedLocked()
}

// Upsert 更新或插入一个玩家到内存，并重新计算排序与 Top1000。
// 调用方应该在成功写入 SQLite 后立即调用此方法。
func (lb *Leaderboard) Upsert(p player.Player) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	key := strings.ToLower(p.OnlineID)
	stored := lb.players[key]
	if stored == nil {
		// 新玩家：分配新指针
		stored = &player.Player{}
		lb.players[key] = stored
	}

	// 更新所有字段
	*stored = p

	lb.rebuildRankedLocked()
}

// Get 返回指定 online_id 的玩家及其排名信息。
// 若不存在返回 nil。
func (lb *Leaderboard) Get(onlineID string) *rank.RankedPlayer {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	key := strings.ToLower(onlineID)
	for i := range lb.ranked {
		if strings.EqualFold(lb.ranked[i].OnlineID, key) {
			result := lb.ranked[i]
			return &result
		}
	}
	return nil
}

// Page 返回分页结果。
// page 从 1 开始，pageSize 为每页大小（通常 50）。
//
// 优化策略：若请求区间完全在 Top1000 内（即 end <= min(1000, totalCount)），
// 则从 top1000 视图读取；否则从全量 ranked 读取，确保不会因越界截断。
func (lb *Leaderboard) Page(page, pageSize int) []rank.RankedPlayer {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	end := offset + pageSize

	totalCount := len(lb.ranked)
	top1000Size := len(lb.top1000)

	// 判断是否可以用 Top1000 视图：
	// 条件：请求区间完全在 [0, min(1000, totalCount)) 内
	useTop1000 := top1000Size > 0 && end <= top1000Size

	if useTop1000 {
		// 使用 Top1000 视图
		if offset >= top1000Size {
			return nil
		}
		if end > top1000Size {
			end = top1000Size
		}
		result := make([]rank.RankedPlayer, end-offset)
		copy(result, lb.top1000[offset:end])
		return result
	}

	// 使用全量 ranked
	if offset >= totalCount {
		return nil
	}
	if end > totalCount {
		end = totalCount
	}
	result := make([]rank.RankedPlayer, end-offset)
	copy(result, lb.ranked[offset:end])
	return result
}

// TotalPages 返回总页数。
func (lb *Leaderboard) TotalPages(pageSize int) int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if pageSize <= 0 || len(lb.ranked) == 0 {
		return 0
	}
	return (len(lb.ranked) + pageSize - 1) / pageSize
}

// Count 返回当前排行榜总玩家数。
func (lb *Leaderboard) Count() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return len(lb.ranked)
}

// ThresholdScore 返回当前第 1000 名的门槛分；不足 1000 人时返回 -1。
// Phase 1 暂不使用，预留给 Phase 2 双路径写入。
func (lb *Leaderboard) ThresholdScore() int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	return lb.thresholdScore
}

// rebuildRankedLocked 重新排序并更新 Top1000 与门槛分。
// 必须持有写锁时调用。
func (lb *Leaderboard) rebuildRankedLocked() {
	// 转换为 rank.Player 切片
	players := make([]rank.Player, 0, len(lb.players))
	for _, p := range lb.players {
		players = append(players, rank.Player{
			OnlineID:  p.OnlineID,
			DisplayID: p.DisplayID,
			AvatarURL: p.AvatarURL,
			Counts: rank.Counts{
				Bronze:   p.Bronze,
				Silver:   p.Silver,
				Gold:     p.Gold,
				Platinum: p.Platinum,
			},
			Score: p.Score,
		})
	}

	// 排序并分配竞赛名次
	lb.ranked = rank.SortAndNumber(players)

	// 更新 Top1000
	if len(lb.ranked) >= 1000 {
		lb.top1000 = make([]rank.RankedPlayer, 1000)
		copy(lb.top1000, lb.ranked[:1000])
		// 门槛分：第 1000 名的 score
		lb.thresholdScore = lb.ranked[999].Score
	} else {
		// 不足 1000 人，Top1000 = 全量
		lb.top1000 = make([]rank.RankedPlayer, len(lb.ranked))
		copy(lb.top1000, lb.ranked)
		lb.thresholdScore = -1 // 表示门槛为 -∞
	}
}
