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
