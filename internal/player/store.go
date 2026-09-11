package player

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Player is the database record for a ranked player.
type Player struct {
	OnlineID  string
	DisplayID string
	AvatarURL string
	Bronze    int
	Silver    int
	Gold      int
	Platinum  int
	Score     int
	JoinedAt  time.Time
	SyncedAt  time.Time
}

// Store manages player persistence.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS players (
    online_id   TEXT PRIMARY KEY COLLATE NOCASE,
    display_id  TEXT NOT NULL,
    avatar_url  TEXT NOT NULL DEFAULT '',
    bronze      INTEGER NOT NULL DEFAULT 0,
    silver      INTEGER NOT NULL DEFAULT 0,
    gold        INTEGER NOT NULL DEFAULT 0,
    platinum    INTEGER NOT NULL DEFAULT 0,
    score       INTEGER NOT NULL DEFAULT 0,
    joined_at   TEXT NOT NULL,
    synced_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_players_score ON players (
    score DESC,
    platinum DESC,
    gold DESC,
    silver DESC,
    bronze DESC,
    display_id ASC
);
`

// Open creates or opens the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Upsert inserts or updates a player. If the player exists, it updates counts,
// score, avatar, display_id, and synced_at, but preserves joined_at.
func (s *Store) Upsert(p Player) error {
	onlineIDLower := strings.ToLower(p.OnlineID)
	now := time.Now().UTC().Format(time.RFC3339)

	var existingJoinedStr string
	err := s.db.QueryRow(`
		SELECT joined_at FROM players WHERE online_id = ?
	`, onlineIDLower).Scan(&existingJoinedStr)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check existing player: %w", err)
	}

	joinedAt := now
	if err == nil {
		joinedAt = existingJoinedStr
	}

	_, err = s.db.Exec(`
		INSERT INTO players (online_id, display_id, avatar_url, bronze, silver, gold, platinum, score, joined_at, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(online_id) DO UPDATE SET
			display_id = excluded.display_id,
			avatar_url = excluded.avatar_url,
			bronze = excluded.bronze,
			silver = excluded.silver,
			gold = excluded.gold,
			platinum = excluded.platinum,
			score = excluded.score,
			synced_at = excluded.synced_at
	`, onlineIDLower, p.DisplayID, p.AvatarURL, p.Bronze, p.Silver, p.Gold, p.Platinum, p.Score, joinedAt, now)

	if err != nil {
		return fmt.Errorf("upsert player: %w", err)
	}
	return nil
}

const (
	// BatchSize 是批量 Upsert 的固定批次大小
	BatchSize = 100
	// SQLite 占位符上限约 32766 (SQLITE_MAX_VARIABLE_NUMBER)
	// 每条记录 10 个字段，32766 / 10 = 3276，但保守设为 1000
	MaxRecordsPerStatement = 1000
)

// UpsertBatch 批量插入或更新玩家，使用批事务 + multi-VALUES。
// 固定批次 N=100（可配置），每批一个事务。
// 同批 multi-VALUES：同一事务内用一条（或少数几条）INSERT ... VALUES (...),(...),... ON CONFLICT ... DO UPDATE SET ...
// 去掉每行前置 SELECT joined_at：用 SQL 保留已有 joined_at。
func (s *Store) UpsertBatch(players []Player) error {
	if len(players) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// 按固定批次 N=100 切块
	for batchStart := 0; batchStart < len(players); batchStart += BatchSize {
		batchEnd := batchStart + BatchSize
		if batchEnd > len(players) {
			batchEnd = len(players)
		}
		batch := players[batchStart:batchEnd]

		// 每组一个事务
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin transaction for batch %d-%d: %w", batchStart, batchEnd, err)
		}

		// 在同一事务内，可能需要拆成多个 multi-VALUES statement（如果驱动参数上限被迫拆分）
		// 每个 statement 最多 MaxRecordsPerStatement 条记录
		for stmtStart := 0; stmtStart < len(batch); stmtStart += MaxRecordsPerStatement {
			stmtEnd := stmtStart + MaxRecordsPerStatement
			if stmtEnd > len(batch) {
				stmtEnd = len(batch)
			}
			chunk := batch[stmtStart:stmtEnd]

			// 构造 multi-VALUES SQL
			// INSERT INTO players (online_id, display_id, avatar_url, bronze, silver, gold, platinum, score, joined_at, synced_at)
			// VALUES (?,?,?,?,?,?,?,?,?,?),(?,?,?,?,?,?,?,?,?,?),...
			// ON CONFLICT(online_id) DO UPDATE SET
			//   display_id = excluded.display_id,
			//   avatar_url = excluded.avatar_url,
			//   bronze = excluded.bronze,
			//   silver = excluded.silver,
			//   gold = excluded.gold,
			//   platinum = excluded.platinum,
			//   score = excluded.score,
			//   joined_at = players.joined_at,  -- 保留已有 joined_at
			//   synced_at = excluded.synced_at

			var builder strings.Builder
			builder.WriteString(`INSERT INTO players (online_id, display_id, avatar_url, bronze, silver, gold, platinum, score, joined_at, synced_at) VALUES `)

			args := make([]interface{}, 0, len(chunk)*10)
			for i, p := range chunk {
				if i > 0 {
					builder.WriteString(",")
				}
				builder.WriteString("(?,?,?,?,?,?,?,?,?,?)")

				onlineIDLower := strings.ToLower(p.OnlineID)
				args = append(args, onlineIDLower, p.DisplayID, p.AvatarURL, p.Bronze, p.Silver, p.Gold, p.Platinum, p.Score, now, now)
			}

			builder.WriteString(` ON CONFLICT(online_id) DO UPDATE SET
				display_id = excluded.display_id,
				avatar_url = excluded.avatar_url,
				bronze = excluded.bronze,
				silver = excluded.silver,
				gold = excluded.gold,
				platinum = excluded.platinum,
				score = excluded.score,
				joined_at = players.joined_at,
				synced_at = excluded.synced_at`)

			if _, err := tx.Exec(builder.String(), args...); err != nil {
				tx.Rollback()
				return fmt.Errorf("exec multi-values for batch %d-%d chunk %d-%d: %w", batchStart, batchEnd, stmtStart, stmtEnd, err)
			}
		}

		// COMMIT
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction for batch %d-%d: %w", batchStart, batchEnd, err)
		}
	}

	return nil
}

// Get retrieves a single player by online ID (case-insensitive).
func (s *Store) Get(onlineID string) (*Player, error) {
	var p Player
	var joinedStr, syncedStr string
	err := s.db.QueryRow(`
		SELECT online_id, display_id, avatar_url, bronze, silver, gold, platinum, score, joined_at, synced_at
		FROM players
		WHERE online_id = ?
	`, strings.ToLower(onlineID)).Scan(
		&p.OnlineID, &p.DisplayID, &p.AvatarURL,
		&p.Bronze, &p.Silver, &p.Gold, &p.Platinum, &p.Score,
		&joinedStr, &syncedStr,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get player: %w", err)
	}
	p.JoinedAt, _ = time.Parse(time.RFC3339, joinedStr)
	p.SyncedAt, _ = time.Parse(time.RFC3339, syncedStr)
	return &p, nil
}

// ListAll returns all players (unsorted). Caller should sort via rank package.
func (s *Store) ListAll() ([]Player, error) {
	rows, err := s.db.Query(`
		SELECT online_id, display_id, avatar_url, bronze, silver, gold, platinum, score, joined_at, synced_at
		FROM players
	`)
	if err != nil {
		return nil, fmt.Errorf("list players: %w", err)
	}
	defer rows.Close()

	var players []Player
	for rows.Next() {
		var p Player
		var joinedStr, syncedStr string
		if err := rows.Scan(
			&p.OnlineID, &p.DisplayID, &p.AvatarURL,
			&p.Bronze, &p.Silver, &p.Gold, &p.Platinum, &p.Score,
			&joinedStr, &syncedStr,
		); err != nil {
			return nil, fmt.Errorf("scan player: %w", err)
		}
		p.JoinedAt, _ = time.Parse(time.RFC3339, joinedStr)
		p.SyncedAt, _ = time.Parse(time.RFC3339, syncedStr)
		players = append(players, p)
	}
	return players, rows.Err()
}

// UpdateSyncedAt updates the synced_at timestamp for a player (test helper).
func (s *Store) UpdateSyncedAt(onlineID string, syncedAt time.Time) error {
	_, err := s.db.Exec(`
		UPDATE players SET synced_at = ? WHERE online_id = ?
	`, syncedAt.UTC().Format(time.RFC3339), strings.ToLower(onlineID))
	if err != nil {
		return fmt.Errorf("update synced_at: %w", err)
	}
	return nil
}
