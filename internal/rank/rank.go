package rank

import "strings"

// Score is Sony's published trophy point formula (bronze 15, silver 30, gold 90, platinum 300).
// This is the canonical score calculation for ranking.
func Score(bronze, silver, gold, platinum int) int {
	return bronze*15 + silver*30 + gold*90 + platinum*300
}

// Counts is the trophy breakdown for one player.
type Counts struct {
	Bronze   int
	Silver   int
	Gold     int
	Platinum int
}

// Player is the ranking input.
type Player struct {
	OnlineID  string
	DisplayID string
	AvatarURL string
	Counts
	Score int
}

// RankedPlayer is a Player with a competition rank.
type RankedPlayer struct {
	Player
	Rank int
}

// SortAndNumber applies the PRD sort order and assigns competition ranks.
// Sort keys (descending): score, platinum, gold, silver, bronze.
// Tiebreaker (ascending): online_id (case-insensitive).
// Competition rank: same counts = same rank, next rank skips (1, 1, 3).
func SortAndNumber(players []Player) []RankedPlayer {
	sorted := make([]Player, len(players))
	copy(sorted, players)

	// Insertion sort for clarity; v1 data volume is small.
	for i := 1; i < len(sorted); i++ {
		j := i
		for j > 0 && less(sorted[j], sorted[j-1]) {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			j--
		}
	}

	ranked := make([]RankedPlayer, len(sorted))
	for i, p := range sorted {
		rank := i + 1
		if i > 0 && sameRank(sorted[i-1], p) {
			rank = ranked[i-1].Rank
		}
		ranked[i] = RankedPlayer{Player: p, Rank: rank}
	}
	return ranked
}

// less returns true if a should come before b in the leaderboard.
func less(a, b Player) bool {
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

// sameRank returns true if two players have identical trophy counts and scores.
func sameRank(a, b Player) bool {
	return a.Score == b.Score &&
		a.Platinum == b.Platinum &&
		a.Gold == b.Gold &&
		a.Silver == b.Silver &&
		a.Bronze == b.Bronze
}

// Page slices the ranked list. Pages are 1-indexed; pageSize is 50 by spec.
// Returns the slice for the requested page (may be empty if page > total).
func Page(ranked []RankedPlayer, page, pageSize int) []RankedPlayer {
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	if offset >= len(ranked) {
		return nil
	}
	end := offset + pageSize
	if end > len(ranked) {
		end = len(ranked)
	}
	return ranked[offset:end]
}

// TotalPages returns the number of pages for the given total count and page size.
func TotalPages(total, pageSize int) int {
	if total == 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}
