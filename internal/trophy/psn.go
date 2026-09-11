package trophy

import (
	"context"
	"ps-trophy-ranking/internal/psn"
)

// PSN wraps the psn.Client without cooldown tracking (cooldown is store-backed).
type PSN struct {
	client *psn.Client
}

// NewPSN creates a PSN trophy source.
func NewPSN(client *psn.Client) *PSN {
	return &PSN{
		client: client,
	}
}

// Lookup fetches trophy summary from PSN.
func (p *PSN) Lookup(ctx context.Context, onlineID string) (*Summary, error) {
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
			case psn.KindInvalidCredentials:
				return nil, NewError(KindInvalidCredentials)
			case psn.KindUpstream:
				return nil, NewError(KindUpstream)
			}
		}
		return nil, NewError(KindUpstream)
	}

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
