package wal

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"ps-trophy-ranking/internal/player"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultShardCount 是默认的 WAL 分片数量
	DefaultShardCount = 10
	// SealedPrefix 是封段文件的前缀
	SealedPrefix = "sealed-"
)

// Event 是 WAL 日志事件
type Event struct {
	OnlineID  string    `json:"online_id"`
	DisplayID string    `json:"display_id"`
	AvatarURL string    `json:"avatar_url"`
	Bronze    int       `json:"bronze"`
	Silver    int       `json:"silver"`
	Gold      int       `json:"gold"`
	Platinum  int       `json:"platinum"`
	Score     int       `json:"score"`
	EventTS   time.Time `json:"event_ts"`
	Seq       int64     `json:"seq"`
}

// Manager 管理 WAL 分片与封段刷盘
type Manager struct {
	dir        string
	shardCount int
	shardFiles []*os.File
	shardMu    []sync.Mutex
	seqCounter int64
	seqMu      sync.Mutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// New 创建新的 WAL Manager
func New(dir string, shardCount int) (*Manager, error) {
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}

	// 确保目录存在
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create wal directory: %w", err)
	}

	m := &Manager{
		dir:        dir,
		shardCount: shardCount,
		shardFiles: make([]*os.File, shardCount),
		shardMu:    make([]sync.Mutex, shardCount),
		stopCh:     make(chan struct{}),
	}

	// 打开或创建所有分片文件
	for i := 0; i < shardCount; i++ {
		shardPath := filepath.Join(dir, fmt.Sprintf("shard-%d.log", i))
		f, err := os.OpenFile(shardPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			// 关闭已打开的文件
			for j := 0; j < i; j++ {
				m.shardFiles[j].Close()
			}
			return nil, fmt.Errorf("open shard file %d: %w", i, err)
		}
		m.shardFiles[i] = f
	}

	return m, nil
}

// Append 追加一个事件到对应的分片
func (m *Manager) Append(p player.Player) error {
	// 根据 online_id hash 选择分片
	shardIdx := m.shardIndex(p.OnlineID)

	// 生成递增的序列号
	m.seqMu.Lock()
	m.seqCounter++
	seq := m.seqCounter
	m.seqMu.Unlock()

	event := Event{
		OnlineID:  p.OnlineID,
		DisplayID: p.DisplayID,
		AvatarURL: p.AvatarURL,
		Bronze:    p.Bronze,
		Silver:    p.Silver,
		Gold:      p.Gold,
		Platinum:  p.Platinum,
		Score:     p.Score,
		EventTS:   time.Now().UTC(),
		Seq:       seq,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	// 追加到对应分片（带换行符）
	m.shardMu[shardIdx].Lock()
	defer m.shardMu[shardIdx].Unlock()

	if _, err := m.shardFiles[shardIdx].Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to shard %d: %w", shardIdx, err)
	}

	return nil
}

// shardIndex 根据 online_id 计算分片索引
func (m *Manager) shardIndex(onlineID string) int {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(onlineID)))
	return int(h.Sum32() % uint32(m.shardCount))
}

// StartSealing 启动定期封段 Worker
func (m *Manager) StartSealing(intervalMS int, flushFunc func([]player.Player) error) {
	if intervalMS <= 0 {
		intervalMS = 100
	}
	interval := time.Duration(intervalMS) * time.Millisecond

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 依次封段、读取、刷盘、删除
				for i := 0; i < m.shardCount; i++ {
					if err := m.sealAndFlush(i, flushFunc); err != nil {
						slog.Error("Failed to seal and flush shard", slog.Int("shard", i), slog.String("error", err.Error()))
					}
				}
			case <-m.stopCh:
				return
			}
		}
	}()
}

// sealAndFlush 执行单个分片的封段、读取、刷盘、删除流程
func (m *Manager) sealAndFlush(shardIdx int, flushFunc func([]player.Player) error) error {
	m.shardMu[shardIdx].Lock()
	defer m.shardMu[shardIdx].Unlock()

	shardPath := filepath.Join(m.dir, fmt.Sprintf("shard-%d.log", shardIdx))
	
	// 检查文件是否有内容
	info, err := os.Stat(shardPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，重新创建
			f, err := os.OpenFile(shardPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("recreate shard file: %w", err)
			}
			m.shardFiles[shardIdx].Close()
			m.shardFiles[shardIdx] = f
			return nil
		}
		return fmt.Errorf("stat shard file: %w", err)
	}

	// 如果文件为空，不需要封段
	if info.Size() == 0 {
		return nil
	}

	// 1. Rename 封段（原子操作）
	sealedPath := filepath.Join(m.dir, fmt.Sprintf("shard-%d.log.%s%d", shardIdx, SealedPrefix, time.Now().Unix()))
	
	// 关闭当前文件句柄
	if err := m.shardFiles[shardIdx].Close(); err != nil {
		slog.Warn("Failed to close shard file before rename", slog.Int("shard", shardIdx), slog.String("error", err.Error()))
	}

	// Rename
	if err := os.Rename(shardPath, sealedPath); err != nil {
		// Rename 失败，重新打开原文件
		f, _ := os.OpenFile(shardPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		m.shardFiles[shardIdx] = f
		return fmt.Errorf("rename to sealed: %w", err)
	}

	// 2. 创建新的空 shard-N.log
	newFile, err := os.OpenFile(shardPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("create new shard file: %w", err)
	}
	m.shardFiles[shardIdx] = newFile

	// 3. 读取 sealed 段
	players, err := m.readSealed(sealedPath)
	if err != nil {
		return fmt.Errorf("read sealed file: %w", err)
	}

	// 4. 如果没有数据，直接删除 sealed 文件
	if len(players) == 0 {
		os.Remove(sealedPath)
		return nil
	}

	// 5. 批量 Upsert（调用外部 flushFunc）
	if err := flushFunc(players); err != nil {
		// 刷盘失败，保留 sealed 文件待重试
		return fmt.Errorf("flush to db: %w", err)
	}

	// 6. 成功后删除 sealed 文件
	if err := os.Remove(sealedPath); err != nil {
		slog.Warn("Failed to remove sealed file", slog.String("path", sealedPath), slog.String("error", err.Error()))
	}

	return nil
}

// readSealed 读取 sealed 文件并去重（保留最新事件）
func (m *Manager) readSealed(path string) ([]player.Player, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open sealed file: %w", err)
	}
	defer f.Close()

	// 读取所有事件
	decoder := json.NewDecoder(f)
	eventsMap := make(map[string]Event) // key: lowercase online_id

	for {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			// 跳过损坏的行
			slog.Warn("Skipping invalid event in sealed file", slog.String("path", path), slog.String("error", err.Error()))
			continue
		}

		key := strings.ToLower(event.OnlineID)
		existing, ok := eventsMap[key]
		
		// 保留最新的事件（Seq 更大或 EventTS 更晚）
		if !ok || event.Seq > existing.Seq {
			eventsMap[key] = event
		} else if event.Seq == existing.Seq && event.EventTS.After(existing.EventTS) {
			eventsMap[key] = event
		}
	}

	// 转换为 player.Player 切片
	players := make([]player.Player, 0, len(eventsMap))
	for _, event := range eventsMap {
		players = append(players, player.Player{
			OnlineID:  event.OnlineID,
			DisplayID: event.DisplayID,
			AvatarURL: event.AvatarURL,
			Bronze:    event.Bronze,
			Silver:    event.Silver,
			Gold:      event.Gold,
			Platinum:  event.Platinum,
			Score:     event.Score,
			SyncedAt:  event.EventTS,
		})
	}

	return players, nil
}

// ReplaySealed 重放所有未处理的 sealed 段（启动时调用）
func (m *Manager) ReplaySealed(flushFunc func([]player.Player) error) error {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在，无需重放
		}
		return fmt.Errorf("read wal directory: %w", err)
	}

	// 收集所有 sealed 文件
	var sealedFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// 匹配 shard-N.log.sealed-timestamp
		if strings.Contains(name, "."+SealedPrefix) {
			sealedFiles = append(sealedFiles, name)
		}
	}

	if len(sealedFiles) == 0 {
		return nil // 无 sealed 文件
	}

	// 按时间戳排序（文件名中的 sealed-<timestamp>）
	sort.Slice(sealedFiles, func(i, j int) bool {
		tsI := extractTimestamp(sealedFiles[i])
		tsJ := extractTimestamp(sealedFiles[j])
		return tsI < tsJ
	})

	slog.Info("Replaying sealed WAL files", slog.Int("count", len(sealedFiles)))

	// 依次重放
	for _, name := range sealedFiles {
		path := filepath.Join(m.dir, name)
		players, err := m.readSealed(path)
		if err != nil {
			slog.Error("Failed to read sealed file", slog.String("name", name), slog.String("error", err.Error()))
			continue
		}

		if len(players) == 0 {
			// 空文件，直接删除
			os.Remove(path)
			continue
		}

		// 批量刷盘
		if err := flushFunc(players); err != nil {
			slog.Error("Replay flush failed", slog.String("name", name), slog.String("error", err.Error()))
			// 失败时不删除文件，下次启动继续重放
			continue
		}

		// 成功后删除
		if err := os.Remove(path); err != nil {
			slog.Warn("Failed to remove replayed sealed file", slog.String("name", name), slog.String("error", err.Error()))
		} else {
			slog.Info("Replayed and removed sealed file", slog.String("name", name))
		}
	}

	return nil
}

// extractTimestamp 从文件名中提取时间戳
func extractTimestamp(filename string) int64 {
	// 文件名格式：shard-N.log.sealed-<timestamp>
	parts := strings.Split(filename, SealedPrefix)
	if len(parts) < 2 {
		return 0
	}
	ts, _ := strconv.ParseInt(parts[1], 10, 64)
	return ts
}

// Stop 停止封段 Worker
func (m *Manager) Stop() {
	close(m.stopCh)
	m.wg.Wait()

	// 关闭所有分片文件
	for i := range m.shardFiles {
		if m.shardFiles[i] != nil {
			m.shardFiles[i].Close()
		}
	}
}

// Close 是 Stop 的别名
func (m *Manager) Close() error {
	m.Stop()
	return nil
}
