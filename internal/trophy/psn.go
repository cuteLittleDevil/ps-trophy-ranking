package trophy

import (
	"context"
	"ps-trophy-ranking/internal/psn"
	"strings"
	"sync"
	"time"
)

// PSN wraps the psn.Client with cooldown tracking.
type PSN struct {
	client   *psn.Client
	cooldown time.Duration

	mu        sync.Mutex
	lastSync  map[string]time.Time
}

// NewPSN creates a PSN trophy source with the given cooldown (15 minutes by spec).
func NewPSN(client *psn.Client, cooldown time.Duration) *PSN {
	return &PSN{
		client:   client,
		cooldown: cooldown,
		lastSync: make(map[string]time.Time),
	}
}

// Lookup fetches trophy summary from PSN. Enforces cooldown.
func (p *PSN) Lookup(ctx context.Context, onlineID string) (*Summary, error) {
	key := strings.ToLower(strings.TrimSpace(onlineID))

	p.mu.Lock()
	last, ok := p.lastSync[key]
	p.mu.Unlock()

	if ok && time.Since(last) < p.cooldown {
		return nil, NewError(KindCooldown)
	}

	summary, err := p.client.Lookup(ctx, onlineID)
	if err != nil {
		if psnErr, ok := err.(*psn.Error); ok {
			switch psnErr.Kind {
			case psn.KindNotFound:
				return nil, NewError(KindNotFound)
			case psn.KindPrivate:
				return nil, NewError(KindPrivate)
			case psn.KindNoCredentials:
				return nil, NewError(KindNoCredentials)
			case psn.KindInvalidCredentials, psn.KindUpstream:
				return nil, NewError(KindUpstream)
			}
		}
		return nil, NewError(KindUpstream)
	}

	p.mu.Lock()
	p.lastSync[key] = time.Now()
	p.mu.Unlock()

	return &Summary{
		OnlineID:  summary.OnlineID,
		DisplayID: summary.DisplayID,
		AvatarURL: summary.AvatarURL,
		Counts: Counts{
			Bronze:   summary.Bronze,
			Silver:   summary.Silver,
			Gold:     summary.Gold,
			Platinum: summary.Platinum,
		},
	}, nil
}

// LastSync returns the last sync time for an online ID.
func (p *PSN) LastSync(onlineID string) time.Time {
	key := strings.ToLower(strings.TrimSpace(onlineID))
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastSync[key]
}
