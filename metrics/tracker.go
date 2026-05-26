package metrics

import (
	"fmt"
	"sync"
	"time"
)

// TokenTracker tracks the number of input and output tokens consumed, and computes costs and daily projections.
type TokenTracker struct {
	mu            sync.RWMutex
	startTime     time.Time
	inputTokens   int64
	outputTokens  int64
	cacheRead     int64
	cacheCreate   int64
}

// NewTokenTracker initializes a new TokenTracker.
func NewTokenTracker() *TokenTracker {
	return &TokenTracker{
		startTime: time.Now(),
	}
}

// AddUsage adds input and output tokens to the tracker.
func (t *TokenTracker) AddUsage(input, output int64, cacheRead int64, cacheCreate int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inputTokens += input
	t.outputTokens += output
	t.cacheRead += cacheRead
	t.cacheCreate += cacheCreate
}

// GetStats returns current token counts and costs.
func (t *TokenTracker) GetStats() (int64, int64, float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Cost: $3.00 / M input, $15.00 / M output. Cache read is cheaper, but let's stick to standard pricing
	// or estimate: Input cost = (inputTokens * 3.0) / 1,000,000; Output cost = (outputTokens * 15.0) / 1,000,000
	inputCost := float64(t.inputTokens) * 3.0 / 1000000.0
	outputCost := float64(t.outputTokens) * 15.0 / 1000000.0
	totalCost := inputCost + outputCost

	return t.inputTokens, t.outputTokens, totalCost
}

// PrintSummary prints a premium visual exit summary of the session statistics.
func (t *TokenTracker) PrintSummary() {
	t.mu.RLock()
	duration := time.Since(t.startTime)
	input := t.inputTokens
	output := t.outputTokens
	total := input + output
	
	inputCost := float64(input) * 3.0 / 1000000.0
	outputCost := float64(output) * 15.0 / 1000000.0
	totalCost := inputCost + outputCost
	t.mu.RUnlock()

	// Projecting daily usage based on the session's average rate
	var projectedDailyCost float64
	var projectedDailyTokens int64
	if duration.Seconds() > 0 {
		tokensPerSec := float64(total) / duration.Seconds()
		projectedDailyTokens = int64(tokensPerSec * 86400)
		costPerSec := totalCost / duration.Seconds()
		projectedDailyCost = costPerSec * 86400
	}

	fmt.Println()
	fmt.Println("\033[1;36m==================================================\033[0m")
	fmt.Println("\033[1;32m         SESSION STATISTICS & USAGE SUMMARY       \033[0m")
	fmt.Println("\033[1;36m==================================================\033[0m")
	fmt.Printf("\033[1mSession Duration:\033[0m      %s\n", duration.Round(time.Second))
	fmt.Printf("\033[1mTokens Sent (Input):\033[0m  %d tokens\n", input)
	fmt.Printf("\033[1mTokens Recv (Output):\033[0m %d tokens\n", output)
	fmt.Printf("\033[1mTotal Tokens:\033[0m         %d tokens\n", total)
	fmt.Println("\033[1;36m--------------------------------------------------\033[0m")
	fmt.Printf("\033[1mEstimated Session Cost:\033[0m  $%.6f USD\n", totalCost)
	fmt.Printf("\033[1mProjected Daily Tokens:\033[0m  %d tokens/day\n", projectedDailyTokens)
	fmt.Printf("\033[1mProjected Daily Cost:\033[0m    $%.4f USD/day\n", projectedDailyCost)
	fmt.Println("\033[0;90m(Daily projection is based on active usage during this session)\033[0m")
	fmt.Println("\033[1;36m==================================================\033[0m")
	fmt.Println()
}
