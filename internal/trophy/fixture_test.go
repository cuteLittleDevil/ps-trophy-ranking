package trophy

import (
	"context"
	"testing"
)

func TestFixtureThreePlayers(t *testing.T) {
	f := NewFixture()
	ctx := context.Background()

	tests := []struct {
		id       string
		wantName string
		wantPt   int
	}{
		{"fixture_alpha", "Fixture_Alpha", 50},
		{"fixture_bravo", "Fixture_Bravo", 40},
		{"fixture_charlie", "Fixture_Charlie", 25},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			s, err := f.Lookup(ctx, tt.id)
			if err != nil {
				t.Fatalf("lookup %s: %v", tt.id, err)
			}
			if s.DisplayID != tt.wantName {
				t.Errorf("DisplayID = %q, want %q", s.DisplayID, tt.wantName)
			}
			if s.Platinum != tt.wantPt {
				t.Errorf("Platinum = %d, want %d", s.Platinum, tt.wantPt)
			}
		})
	}
}

func TestFixtureNotFound(t *testing.T) {
	f := NewFixture()
	ctx := context.Background()

	s, err := f.Lookup(ctx, "unknown_player")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if s != nil {
		t.Errorf("expected nil summary, got %+v", s)
	}

	var tErr *Error
	if !isError(err, &tErr) || tErr.Kind != KindNotFound {
		t.Errorf("expected KindNotFound error, got %v", err)
	}
}

func TestFixtureCaseInsensitive(t *testing.T) {
	f := NewFixture()
	ctx := context.Background()

	s, err := f.Lookup(ctx, "FIXTURE_ALPHA")
	if err != nil {
		t.Fatalf("lookup uppercase: %v", err)
	}
	if s.DisplayID != "Fixture_Alpha" {
		t.Errorf("DisplayID = %q, want %q", s.DisplayID, "Fixture_Alpha")
	}
}

func TestFixtureNoCooldown(t *testing.T) {
	f := NewFixture()
	ctx := context.Background()
	
	// Fixture mode: no cooldown, can call multiple times
	_, err := f.Lookup(ctx, "fixture_alpha")
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	
	_, err = f.Lookup(ctx, "fixture_alpha")
	if err != nil {
		t.Fatalf("second lookup should succeed: %v", err)
	}
}

func isError(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	tErr, ok := target.(**Error)
	if !ok {
		return false
	}
	e, ok := err.(*Error)
	if !ok {
		return false
	}
	*tErr = e
	return true
}
