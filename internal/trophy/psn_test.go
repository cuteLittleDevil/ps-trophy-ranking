package trophy

import (
	"context"
	"ps-trophy-ranking/internal/psn"
	"testing"
)

func TestPSNErrorMapping(t *testing.T) {
	client := psn.NewWithEndpoints("", psn.Endpoints{})
	psnSource := NewPSN(client)

	ctx := context.Background()

	_, err := psnSource.Lookup(ctx, "testuser")
	if err == nil {
		t.Fatal("expected error with empty NPSSO")
	}

	tErr, ok := err.(*Error)
	if !ok || tErr.Kind != KindNoCredentials {
		t.Errorf("expected KindNoCredentials, got %v", err)
	}
}
