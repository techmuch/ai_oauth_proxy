package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type EngineEvent struct {
	Type         string // "delta", "result", "error", "usage"
	Text         string
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheCreate  int64
	Error        error
}

// StreamLine represents a single line of JSON-stream output from Claude Code CLI.
type StreamLine struct {
	Type       string       `json:"type"`
	Subtype    string       `json:"subtype"`
	Event      *StreamEvent `json:"event"`
	Result     string       `json:"result"`
	Usage      *UsageInfo   `json:"usage"`
}

type StreamEvent struct {
	Type  string       `json:"type"`
	Delta *StreamDelta `json:"delta"`
}

type StreamDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type UsageInfo struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// FlattenMessages flattens an array of OpenAI messages into a system prompt and a transcript payload.
func FlattenMessages(messages []Message) (string, string) {
	var systemPrompt string
	var transcript string

	for _, msg := range messages {
		if msg.Role == "system" {
			if systemPrompt != "" {
				systemPrompt += "\n"
			}
			systemPrompt += msg.Content
		} else {
			roleLabel := "User"
			if msg.Role == "assistant" {
				roleLabel = "Assistant"
			}
			// Prefix the transcript with the system prompt or conversation roles
			transcript += fmt.Sprintf("%s: %s\n\n", roleLabel, msg.Content)
		}
	}

	return systemPrompt, transcript
}

// RunClaude runs the claude CLI as a subprocess and streams back events via a channel.
func RunClaude(ctx context.Context, systemPrompt, transcript string) (<-chan EngineEvent, error) {
	outChan := make(chan EngineEvent, 100)

	// Prepare arguments for Claude Code
	args := []string{
		"-p",
		"--output-format=stream-json",
		"--verbose",
		"--include-partial-messages",
	}

	if systemPrompt != "" {
		args = append(args, "--system-prompt", systemPrompt)
	}

	// Spawns `/root/.local/bin/claude`
	cmd := exec.CommandContext(ctx, "/root/.local/bin/claude", args...)
	
	// Create a new process group for the command so we can kill all child processes on cancel
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Pipe stdin for sending the transcript context
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// Pipe stdout for reading the stream-json
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Pipe stderr to capture errors
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start claude command: %w", err)
	}

	// Manage process group cancellation in background
	cleanupDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			// Kill the entire process group
			if cmd.Process != nil {
				pgid, err := syscall.Getpgid(cmd.Process.Pid)
				if err == nil {
					_ = syscall.Kill(-pgid, syscall.SIGKILL)
				} else {
					_ = cmd.Process.Kill()
				}
			}
		case <-cleanupDone:
		}
	}()

	// Write transcript to stdin in a separate goroutine
	go func() {
		defer stdinPipe.Close()
		_, _ = io.WriteString(stdinPipe, transcript)
	}()

	// Read stderr in a separate goroutine for logging/debugging
	stderrScanner := bufio.NewScanner(stderrPipe)
	go func() {
		for stderrScanner.Scan() {
			// We can log stderr or ignore it to avoid cluttering, but let's check for errors
		}
	}()

	// Read stdout line-by-line and parse the stream-json
	go func() {
		defer close(outChan)
		defer close(cleanupDone)

		scanner := bufio.NewScanner(stdoutPipe)
		// We set a large buffer for the scanner just in case
		const maxCapacity = 1024 * 1024 // 1MB
		buf := make([]byte, maxCapacity)
		scanner.Buffer(buf, maxCapacity)

		for scanner.Scan() {
			lineBytes := scanner.Bytes()
			if len(lineBytes) == 0 {
				continue
			}

			// Parse JSON line
			var sl StreamLine
			if err := json.Unmarshal(lineBytes, &sl); err != nil {
				// Sometimes we get standard non-JSON text output or warnings on stdout, ignore parsing errors
				continue
			}

			// Process event types
			switch sl.Type {
			case "stream_event":
				if sl.Event != nil && sl.Event.Type == "content_block_delta" && sl.Event.Delta != nil {
					outChan <- EngineEvent{
						Type: "delta",
						Text: sl.Event.Delta.Text,
					}
				}
			case "result":
				// Final summary and token usage
				if sl.Usage != nil {
					outChan <- EngineEvent{
						Type:         "usage",
						InputTokens:  sl.Usage.InputTokens,
						OutputTokens: sl.Usage.OutputTokens,
						CacheRead:    sl.Usage.CacheReadInputTokens,
						CacheCreate:  sl.Usage.CacheCreationInputTokens,
					}
				}
				outChan <- EngineEvent{
					Type: "result",
					Text: sl.Result,
				}
			}
		}

		if err := scanner.Err(); err != nil {
			outChan <- EngineEvent{
				Type:  "error",
				Error: err,
			}
		}

		// Wait for process to exit
		_ = cmd.Wait()
	}()

	return outChan, nil
}
