package trophy

import (
	"context"
	"strings"
)

// Fixture is a trophy source with hardcoded demo players. No cooldown, no network.
type Fixture struct {
	players map[string]Summary
}

// NewFixture returns a fixture source with three demo players.
func NewFixture() *Fixture {
	return &Fixture{
		players: map[string]Summary{
			"fixture_alpha": {
				OnlineID:  "fixture_alpha",
				DisplayID: "Fixture_Alpha",
				AvatarURL: "",
				Counts: Counts{
					Bronze:   1000,
					Silver:   500,
					Gold:     200,
					Platinum: 50,
				},
			},
			"fixture_bravo": {
				OnlineID:  "fixture_bravo",
				DisplayID: "Fixture_Bravo",
				AvatarURL: "",
				Counts: Counts{
					Bronze:   800,
					Silver:   400,
					Gold:     150,
					Platinum: 40,
				},
			},
			"fixture_charlie": {
				OnlineID:  "fixture_charlie",
				DisplayID: "Fixture_Charlie",
				AvatarURL: "",
				Counts: Counts{
					Bronze:   500,
					Silver:   250,
					Gold:     100,
					Platinum: 25,
				},
			},
		},
	}
}

// Lookup returns a fixed player or not_found.
func (f *Fixture) Lookup(ctx context.Context, onlineID string) (*Summary, error) {
	key := strings.ToLower(strings.TrimSpace(onlineID))
	p, ok := f.players[key]
	if !ok {
		return nil, NewError(KindNotFound)
	}
	return &p, nil
}
