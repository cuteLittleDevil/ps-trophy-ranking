package trophy

import (
	"context"
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

// Source provides trophy lookups. Production implementation: PSN. Tests inject a fake Source.
type Source interface {
	Lookup(ctx context.Context, onlineID string) (*Summary, error)
}

// Error kinds match psn.Kind for consistent UI mapping.
type ErrorKind string

const (
	KindNotFound           ErrorKind = "not_found"
	KindPrivate            ErrorKind = "private"
	KindNoCredentials      ErrorKind = "no_credentials"
	KindInvalidCredentials ErrorKind = "invalid_credentials"
	KindUpstream           ErrorKind = "upstream"
	KindCooldown           ErrorKind = "cooldown"
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
