package metrics

import (
	"testing"
)

func TestTokenTracker(t *testing.T) {
	tracker := NewTokenTracker()

	// Verify initial stats are zero
	input, output, cost := tracker.GetStats()
	if input != 0 || output != 0 || cost != 0 {
		t.Errorf("expected initial stats to be zero, got input=%d, output=%d, cost=%f", input, output, cost)
	}

	// Add usage
	tracker.AddUsage(1000, 2000, 0, 0)

	// Verify stats
	input, output, cost = tracker.GetStats()
	if input != 1000 {
		t.Errorf("expected 1000 input tokens, got %d", input)
	}
	if output != 2000 {
		t.Errorf("expected 2000 output tokens, got %d", output)
	}

	// Cost calculation: $3.00/M input, $15.00/M output
	// (1000 * 3.0 / 1000000) + (2000 * 15.0 / 1000000) = 0.003 + 0.030 = 0.033
	expectedCost := 0.033
	if cost != expectedCost {
		t.Errorf("expected cost to be %f, got %f", expectedCost, cost)
	}
}
