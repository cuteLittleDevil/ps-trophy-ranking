package trophy

import (
	"context"
	"time"
)

// Counts is the trophy breakdown.
type Counts struct {
	Bronze   int
	Silver   int
	Gold     int
	Platinum int
}

// Summary is the trophy data for one player.
type Summary struct {
	OnlineID  string
	DisplayID string
	AvatarURL string
	Counts
}

// Source provides trophy lookups. Implementations: fixture, PSN.
type Source interface {
	Lookup(ctx context.Context, onlineID string) (*Summary, error)
	// LastSync returns the last sync time for an online ID, or zero if never synced.
	LastSync(onlineID string) time.Time
}

// Error kinds match psn.Kind for consistent UI mapping.
type ErrorKind string

const (
	KindNotFound      ErrorKind = "not_found"
	KindPrivate       ErrorKind = "private"
	KindNoCredentials ErrorKind = "no_credentials"
	KindUpstream      ErrorKind = "upstream"
	KindCooldown      ErrorKind = "cooldown"
)

// Error is a typed trophy lookup failure.
type Error struct {
	Kind ErrorKind
}

func (e *Error) Error() string {
	return string(e.Kind)
}

func NewError(kind ErrorKind) *Error {
	return &Error{Kind: kind}
}
