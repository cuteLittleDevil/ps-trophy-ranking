package psn

import "testing"

func TestScore(t *testing.T) {
	t.Parallel()
	got := Score(1, 1, 1, 1)
	if got != 15+30+90+300 {
		t.Fatalf("Score() = %d", got)
	}
	if Score(0, 0, 0, 0) != 0 {
		t.Fatal("zero trophies must score 0")
	}
}
