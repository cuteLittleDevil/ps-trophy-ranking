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

	// 玩家下标索引，用于快速定位（key 为小写 online_id）
	indexByID map[string]int
}

// New 创建空的内存排行榜。
func New() *Leaderboard {
	return &Leaderboard{
		players:        make(map[string]*player.Player),
		ranked:         []rank.RankedPlayer{},
		top1000:        []rank.RankedPlayer{},
		thresholdScore: -1, // -∞ 表示所有写入走热路径
		indexByID:      make(map[string]int),
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
//
// Phase 1 实现注意：本方法每次调用都会触发全量排序（O(N log N)）。
// 对于真实 PSN 入榜/刷新（低频，15 分钟冷却），这是可接受的。
// Phase 2 将引入 WAL 双路径写入，减少热路径冲击。
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

// UpsertBatch 批量更新或插入玩家到内存，使用有序合并避免全量排序。
// 适用于 WAL flush 场景，批量大小通常为 10-1000 条。
// 性能：对于批量 M 和总量 N，复杂度为 O(M log M + N)，优于多次 Upsert 的 O(M * N log N)。
func (lb *Leaderboard) UpsertBatch(batch []player.Player) {
	if len(batch) == 0 {
		return
	}

	lb.mu.Lock()
	defer lb.mu.Unlock()

	// 1. 更新 players map
	for i := range batch {
		p := &batch[i]
		key := strings.ToLower(p.OnlineID)
		stored := lb.players[key]
		if stored == nil {
			stored = &player.Player{}
			lb.players[key] = stored
		}
		*stored = *p
	}

	// 2. 从 ranked 中移除批次中的玩家（使用 indexByID 快速定位）
	toRemove := make(map[string]bool, len(batch))
	for i := range batch {
		key := strings.ToLower(batch[i].OnlineID)
		toRemove[key] = true
	}

	filtered := make([]rank.RankedPlayer, 0, len(lb.ranked))
	for i := range lb.ranked {
		key := strings.ToLower(lb.ranked[i].OnlineID)
		if !toRemove[key] {
			filtered = append(filtered, lb.ranked[i])
		}
	}

	// 3. 将批次转换为 rank.Player 并排序
	batchPlayers := make([]rank.Player, len(batch))
	for i := range batch {
		batchPlayers[i] = rank.Player{
			OnlineID:  batch[i].OnlineID,
			DisplayID: batch[i].DisplayID,
			AvatarURL: batch[i].AvatarURL,
			Counts: rank.Counts{
				Bronze:   batch[i].Bronze,
				Silver:   batch[i].Silver,
				Gold:     batch[i].Gold,
				Platinum: batch[i].Platinum,
			},
			Score: batch[i].Score,
		}
	}
	batchRanked := rank.SortAndNumber(batchPlayers)

	// 4. 合并 filtered 和 batchRanked（两路归并）
	lb.ranked = lb.mergeSortedLocked(filtered, batchRanked)

	// 5. 重新分配竞赛名次
	lb.assignCompetitionRanksLocked()

	// 6. 更新 Top1000、threshold、indexByID
	lb.updateTop1000AndIndexLocked()
}

// Get 返回指定 online_id 的玩家及其排名信息。
// 若不存在返回 nil。
func (lb *Leaderboard) Get(onlineID string) *rank.RankedPlayer {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	key := strings.ToLower(onlineID)
	if idx, ok := lb.indexByID[key]; ok {
		result := lb.ranked[idx]
		return &result
	}
	return nil
}

// IndexOf 返回指定 online_id 在有序列表中的下标（0-based）。
// 若不存在返回 -1。
// 用于计算分页：page = (index / pageSize) + 1
//
// 重要：必须用下标而非竞赛名次 Rank 计算页码，因为同分玩家 Rank 相同但下标不同。
func (lb *Leaderboard) IndexOf(onlineID string) int {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	key := strings.ToLower(onlineID)
	if idx, ok := lb.indexByID[key]; ok {
		return idx
	}
	return -1
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

	// 更新 Top1000、threshold、indexByID
	lb.updateTop1000AndIndexLocked()
}

// mergeSortedLocked 合并两个已排序的 RankedPlayer 列表（忽略原有的 Rank 字段）。
// 返回按相同排序规则合并后的新列表（Rank 字段需重新分配）。
// 必须持有写锁时调用。
func (lb *Leaderboard) mergeSortedLocked(a, b []rank.RankedPlayer) []rank.RankedPlayer {
	merged := make([]rank.RankedPlayer, 0, len(a)+len(b))
	i, j := 0, 0

	for i < len(a) && j < len(b) {
		// 使用 rank 包的比较逻辑（注意：less 返回 true 表示 a < b）
		// 我们需要判断 a[i] 是否应该排在 b[j] 前面
		aPlayer := rank.Player{
			OnlineID:  a[i].OnlineID,
			DisplayID: a[i].DisplayID,
			AvatarURL: a[i].AvatarURL,
			Counts:    a[i].Counts,
			Score:     a[i].Score,
		}
		bPlayer := rank.Player{
			OnlineID:  b[j].OnlineID,
			DisplayID: b[j].DisplayID,
			AvatarURL: b[j].AvatarURL,
			Counts:    b[j].Counts,
			Score:     b[j].Score,
		}

		// comparePlayers 在 rank 包中不导出，我们直接用排序规则比较
		if shouldComeBefore(aPlayer, bPlayer) {
			merged = append(merged, a[i])
			i++
		} else {
			merged = append(merged, b[j])
			j++
		}
	}

	// 追加剩余元素
	for i < len(a) {
		merged = append(merged, a[i])
		i++
	}
	for j < len(b) {
		merged = append(merged, b[j])
		j++
	}

	return merged
}

// shouldComeBefore 判断 a 是否应该排在 b 前面（复用 rank 包的排序规则）。
func shouldComeBefore(a, b rank.Player) bool {
	// 高分在前
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	if a.Platinum != b.Platinum {
		return a.Platinum > b.Platinum
	}
	if a.Gold != b.Gold {
		return a.Gold > b.Gold
	}
	if a.Silver != b.Silver {
		return a.Silver > b.Silver
	}
	if a.Bronze != b.Bronze {
		return a.Bronze > b.Bronze
	}
	// ID 字典序升序（小写）
	return strings.ToLower(a.OnlineID) < strings.ToLower(b.OnlineID)
}

// assignCompetitionRanksLocked 重新分配竞赛名次（1, 1, 3 规则）。
// 必须在 ranked 已排序后调用，持有写锁。
func (lb *Leaderboard) assignCompetitionRanksLocked() {
	for i := range lb.ranked {
		rank := i + 1
		if i > 0 && sameRankCounts(lb.ranked[i-1], lb.ranked[i]) {
			rank = lb.ranked[i-1].Rank
		}
		lb.ranked[i].Rank = rank
	}
}

// sameRankCounts 判断两个玩家的成绩是否相同（用于竞赛名次）。
func sameRankCounts(a, b rank.RankedPlayer) bool {
	return a.Score == b.Score &&
		a.Platinum == b.Platinum &&
		a.Gold == b.Gold &&
		a.Silver == b.Silver &&
		a.Bronze == b.Bronze
}

// updateTop1000AndIndexLocked 更新 Top1000 视图、门槛分和 indexByID。
// 必须持有写锁时调用。
func (lb *Leaderboard) updateTop1000AndIndexLocked() {
	// 更新 Top1000
	if len(lb.ranked) >= 1000 {
		lb.top1000 = make([]rank.RankedPlayer, 1000)
		copy(lb.top1000, lb.ranked[:1000])
		lb.thresholdScore = lb.ranked[999].Score
	} else {
		lb.top1000 = make([]rank.RankedPlayer, len(lb.ranked))
		copy(lb.top1000, lb.ranked)
		lb.thresholdScore = -1
	}

	// 更新 indexByID
	lb.indexByID = make(map[string]int, len(lb.ranked))
	for i := range lb.ranked {
		key := strings.ToLower(lb.ranked[i].OnlineID)
		lb.indexByID[key] = i
	}
}
