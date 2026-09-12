package memrank

import (
	"log/slog"
	"ps-trophy-ranking/internal/player"
	"ps-trophy-ranking/internal/rank"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// face 是 Left-Right 中的一面只读有序视图（ranked / top1000 / index）。
// 读路径对该面 RLock；写路径仅在该面作为暗面或追平亮面时 WLock。
type face struct {
	mu sync.RWMutex

	ranked         []rank.RankedPlayer
	top1000        []rank.RankedPlayer
	thresholdScore int
	indexByID      map[string]int
}

func newFace() *face {
	return &face{
		ranked:         []rank.RankedPlayer{},
		top1000:        []rank.RankedPlayer{},
		thresholdScore: -1,
		indexByID:      make(map[string]int),
	}
}

// Leaderboard 维护全量内存排行榜与 Top1000 视图。
// 并发模型（SPEC §13.10 Left-Right）：
//   - 两份 face 交替：更新暗面 → atomic 切换读指针 → 追平另一面
//   - 读只通过当前读指针 + 该面 RLock，不被暗面合并的长耗时挡住
//   - players 权威 map 单份，由 writerMu 保护
type Leaderboard struct {
	writerMu sync.Mutex

	players map[string]*player.Player

	faces [2]*face
	read  atomic.Pointer[face]
}

// New 创建空的内存排行榜（双面一致为空，读指针指向 faces[0]）。
func New() *Leaderboard {
	lb := &Leaderboard{
		players: make(map[string]*player.Player),
		faces:   [2]*face{newFace(), newFace()},
	}
	lb.read.Store(lb.faces[0])
	return lb
}

func (lb *Leaderboard) otherFace(current *face) *face {
	if current == lb.faces[0] {
		return lb.faces[1]
	}
	return lb.faces[0]
}

// Load 从玩家列表加载到内存，并初始化双面 Top1000 与门槛分。
func (lb *Leaderboard) Load(players []player.Player) {
	lb.writerMu.Lock()
	defer lb.writerMu.Unlock()

	lb.players = make(map[string]*player.Player, len(players))
	for i := range players {
		p := players[i]
		cp := p
		lb.players[strings.ToLower(p.OnlineID)] = &cp
	}

	for _, f := range lb.faces {
		f.mu.Lock()
		lb.rebuildFaceFromPlayersLocked(f)
		f.mu.Unlock()
	}
	lb.read.Store(lb.faces[0])
	lb.logMemoryEstimate("load")
}

// Upsert 更新或插入一个玩家到内存（Left-Right 写协议）。
func (lb *Leaderboard) Upsert(p player.Player) {
	lb.writerMu.Lock()
	defer lb.writerMu.Unlock()

	key := strings.ToLower(p.OnlineID)
	stored := lb.players[key]
	if stored == nil {
		stored = &player.Player{}
		lb.players[key] = stored
	}
	*stored = p

	lb.publishRebuildBothFaces()
}

// UpsertBatch 批量更新内存，使用有序合并；Left-Right 写协议避免读被长写锁堵住。
func (lb *Leaderboard) UpsertBatch(batch []player.Player) {
	if len(batch) == 0 {
		return
	}

	lb.writerMu.Lock()
	defer lb.writerMu.Unlock()

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

	current := lb.read.Load()
	dark := lb.otherFace(current)
	rankedBefore := 0
	if current != nil {
		current.mu.RLock()
		rankedBefore = len(current.ranked)
		current.mu.RUnlock()
	}

	startDark := time.Now()
	dark.mu.Lock()
	applyBatchToFaceLocked(dark, batch)
	darkN := len(dark.ranked)
	dark.mu.Unlock()
	darkDur := time.Since(startDark)

	startPub := time.Now()
	old := lb.read.Swap(dark)
	pubDur := time.Since(startPub)

	startLight := time.Now()
	old.mu.Lock()
	applyBatchToFaceLocked(old, batch)
	old.mu.Unlock()
	lightDur := time.Since(startLight)

	slog.Info("memrank left-right upsert batch done",
		slog.Int("batch_size", len(batch)),
		slog.Int("ranked_before", rankedBefore),
		slog.Int("ranked_after", darkN),
		slog.Duration("update_dark_ms", darkDur),
		slog.Duration("publish_ms", pubDur),
		slog.Duration("sync_light_ms", lightDur),
	)
}

// publishRebuildBothFaces 用 players 全量重建双面并发布 faces[0]（单条 Upsert / 低频路径）。
func (lb *Leaderboard) publishRebuildBothFaces() {
	startDark := time.Now()
	current := lb.read.Load()
	dark := lb.otherFace(current)
	dark.mu.Lock()
	lb.rebuildFaceFromPlayersLocked(dark)
	darkN := len(dark.ranked)
	dark.mu.Unlock()
	darkDur := time.Since(startDark)

	startPub := time.Now()
	old := lb.read.Swap(dark)
	pubDur := time.Since(startPub)

	startLight := time.Now()
	old.mu.Lock()
	lb.rebuildFaceFromPlayersLocked(old)
	old.mu.Unlock()
	lightDur := time.Since(startLight)

	slog.Info("memrank left-right upsert rebuild done",
		slog.Int("ranked_after", darkN),
		slog.Duration("update_dark_ms", darkDur),
		slog.Duration("publish_ms", pubDur),
		slog.Duration("sync_light_ms", lightDur),
	)
}

func (lb *Leaderboard) withReadFace(fn func(f *face)) {
	f := lb.read.Load()
	f.mu.RLock()
	defer f.mu.RUnlock()
	fn(f)
}

// Get 返回指定 online_id 的玩家及其排名信息。
func (lb *Leaderboard) Get(onlineID string) *rank.RankedPlayer {
	var result *rank.RankedPlayer
	lb.withReadFace(func(f *face) {
		key := strings.ToLower(onlineID)
		if idx, ok := f.indexByID[key]; ok {
			cp := f.ranked[idx]
			result = &cp
		}
	})
	return result
}

// IndexOf 返回指定 online_id 在有序列表中的下标（0-based）。
func (lb *Leaderboard) IndexOf(onlineID string) int {
	idx := -1
	lb.withReadFace(func(f *face) {
		key := strings.ToLower(onlineID)
		if i, ok := f.indexByID[key]; ok {
			idx = i
		}
	})
	return idx
}

// Page 返回分页结果。
func (lb *Leaderboard) Page(page, pageSize int) []rank.RankedPlayer {
	var result []rank.RankedPlayer
	lb.withReadFace(func(f *face) {
		if page < 1 {
			page = 1
		}
		offset := (page - 1) * pageSize
		end := offset + pageSize

		totalCount := len(f.ranked)
		top1000Size := len(f.top1000)
		useTop1000 := top1000Size > 0 && end <= top1000Size

		if useTop1000 {
			if offset >= top1000Size {
				return
			}
			if end > top1000Size {
				end = top1000Size
			}
			result = make([]rank.RankedPlayer, end-offset)
			copy(result, f.top1000[offset:end])
			return
		}

		if offset >= totalCount {
			return
		}
		if end > totalCount {
			end = totalCount
		}
		result = make([]rank.RankedPlayer, end-offset)
		copy(result, f.ranked[offset:end])
	})
	return result
}

// TotalPages 返回总页数。
func (lb *Leaderboard) TotalPages(pageSize int) int {
	pages := 0
	lb.withReadFace(func(f *face) {
		if pageSize <= 0 || len(f.ranked) == 0 {
			return
		}
		pages = (len(f.ranked) + pageSize - 1) / pageSize
	})
	return pages
}

// Count 返回当前排行榜总玩家数。
func (lb *Leaderboard) Count() int {
	n := 0
	lb.withReadFace(func(f *face) {
		n = len(f.ranked)
	})
	return n
}

// ThresholdScore 返回当前第 1000 名的门槛分；不足 1000 人时返回 -1。
func (lb *Leaderboard) ThresholdScore() int {
	s := -1
	lb.withReadFace(func(f *face) {
		s = f.thresholdScore
	})
	return s
}

func (lb *Leaderboard) rebuildFaceFromPlayersLocked(f *face) {
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
	f.ranked = rank.SortAndNumber(players)
	updateTop1000AndIndexLocked(f)
}

func applyBatchToFaceLocked(f *face, batch []player.Player) {
	toRemove := make(map[string]bool, len(batch))
	for i := range batch {
		toRemove[strings.ToLower(batch[i].OnlineID)] = true
	}

	filtered := make([]rank.RankedPlayer, 0, len(f.ranked))
	for i := range f.ranked {
		key := strings.ToLower(f.ranked[i].OnlineID)
		if !toRemove[key] {
			filtered = append(filtered, f.ranked[i])
		}
	}

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
	f.ranked = mergeSorted(filtered, batchRanked)
	assignCompetitionRanks(f.ranked)
	updateTop1000AndIndexLocked(f)
}

func mergeSorted(a, b []rank.RankedPlayer) []rank.RankedPlayer {
	merged := make([]rank.RankedPlayer, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		aPlayer := rank.Player{
			OnlineID: a[i].OnlineID, DisplayID: a[i].DisplayID, AvatarURL: a[i].AvatarURL,
			Counts: a[i].Counts, Score: a[i].Score,
		}
		bPlayer := rank.Player{
			OnlineID: b[j].OnlineID, DisplayID: b[j].DisplayID, AvatarURL: b[j].AvatarURL,
			Counts: b[j].Counts, Score: b[j].Score,
		}
		if shouldComeBefore(aPlayer, bPlayer) {
			merged = append(merged, a[i])
			i++
		} else {
			merged = append(merged, b[j])
			j++
		}
	}
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

func shouldComeBefore(a, b rank.Player) bool {
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
	return strings.ToLower(a.OnlineID) < strings.ToLower(b.OnlineID)
}

func assignCompetitionRanks(ranked []rank.RankedPlayer) {
	for i := range ranked {
		r := i + 1
		if i > 0 && sameRankCounts(ranked[i-1], ranked[i]) {
			r = ranked[i-1].Rank
		}
		ranked[i].Rank = r
	}
}

func sameRankCounts(a, b rank.RankedPlayer) bool {
	return a.Score == b.Score &&
		a.Platinum == b.Platinum &&
		a.Gold == b.Gold &&
		a.Silver == b.Silver &&
		a.Bronze == b.Bronze
}

func updateTop1000AndIndexLocked(f *face) {
	if len(f.ranked) >= 1000 {
		f.top1000 = make([]rank.RankedPlayer, 1000)
		copy(f.top1000, f.ranked[:1000])
		f.thresholdScore = f.ranked[999].Score
	} else {
		f.top1000 = make([]rank.RankedPlayer, len(f.ranked))
		copy(f.top1000, f.ranked)
		f.thresholdScore = -1
	}
	f.indexByID = make(map[string]int, len(f.ranked))
	for i := range f.ranked {
		f.indexByID[strings.ToLower(f.ranked[i].OnlineID)] = i
	}
}

func (lb *Leaderboard) logMemoryEstimate(reason string) {
	f := lb.read.Load()
	f.mu.RLock()
	defer f.mu.RUnlock()

	var stringBytes int
	for i := range f.ranked {
		p := &f.ranked[i]
		stringBytes += len(p.OnlineID) + len(p.DisplayID) + len(p.AvatarURL)
	}
	headerBytes := len(f.ranked) * int(unsafe.Sizeof(rank.RankedPlayer{}))
	if cap(f.ranked) > len(f.ranked) {
		headerBytes = cap(f.ranked) * int(unsafe.Sizeof(rank.RankedPlayer{}))
	}
	slog.Info("memrank memory estimate",
		slog.String("reason", reason),
		slog.Int("ranked_n", len(f.ranked)),
		slog.Int("ranked_header_bytes", headerBytes),
		slog.Int("ranked_string_bytes", stringBytes),
		slog.Int("dual_face_budget_bytes", 2*(headerBytes+stringBytes)),
	)
}
