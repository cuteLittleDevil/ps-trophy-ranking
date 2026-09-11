package trophy

import (
	"context"
	"ps-trophy-ranking/internal/psn"
	"testing"
	"time"
)

func TestPSNCooldown(t *testing.T) {
	client := psn.NewWithEndpoints("", psn.Endpoints{})
	psnSource := NewPSN(client, 100*time.Millisecond)

	ctx := context.Background()

	_, err := psnSource.Lookup(ctx, "testuser")
	if err == nil {
		t.Fatal("expected error with empty NPSSO")
	}

	var tErr *Error
	if !isError(err, &tErr) || tErr.Kind != KindNoCredentials {
		t.Errorf("expected KindNoCredentials, got %v", err)
	}

	psnSource.mu.Lock()
	psnSource.lastSync["testuser"] = time.Now()
	psnSource.mu.Unlock()

	_, err = psnSource.Lookup(ctx, "testuser")
	if err == nil {
		t.Fatal("expected cooldown error")
	}
	if !isError(err, &tErr) || tErr.Kind != KindCooldown {
		t.Errorf("expected KindCooldown, got %v", err)
	}

	time.Sleep(110 * time.Millisecond)

	_, err = psnSource.Lookup(ctx, "testuser")
	if err == nil {
		t.Fatal("expected error with empty NPSSO")
	}
	if !isError(err, &tErr) || tErr.Kind == KindCooldown {
		t.Errorf("should not be cooldown after expiry, got %v", err)
	}
}

func TestPSNLastSync(t *testing.T) {
	client := psn.NewWithEndpoints("", psn.Endpoints{})
	psnSource := NewPSN(client, time.Hour)

	if !psnSource.LastSync("unknown").IsZero() {
		t.Error("LastSync should return zero for unknown ID")
	}

	now := time.Now()
	psnSource.mu.Lock()
	psnSource.lastSync["known"] = now
	psnSource.mu.Unlock()

	got := psnSource.LastSync("known")
	if !got.Equal(now) {
		t.Errorf("LastSync = %v, want %v", got, now)
	}
}
