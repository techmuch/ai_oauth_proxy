package engine

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlattenMessages(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are a coding wizard."},
		{Role: "user", Content: "Write a function."},
		{Role: "assistant", Content: "Here is your function."},
		{Role: "user", Content: "Thanks!"},
	}

	systemPrompt, transcript := FlattenMessages(messages)

	expectedSystem := "You are a coding wizard."
	if systemPrompt != expectedSystem {
		t.Errorf("expected system prompt %q, got %q", expectedSystem, systemPrompt)
	}

	if !strings.Contains(transcript, "User: Write a function.") {
		t.Errorf("transcript missing user prompt: %s", transcript)
	}
	if !strings.Contains(transcript, "Assistant: Here is your function.") {
		t.Errorf("transcript missing assistant prompt: %s", transcript)
	}
	if !strings.Contains(transcript, "User: Thanks!") {
		t.Errorf("transcript missing last prompt: %s", transcript)
	}
}

func TestStreamLineParsing(t *testing.T) {
	deltaLine := `{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}}`
	var sl StreamLine
	if err := json.Unmarshal([]byte(deltaLine), &sl); err != nil {
		t.Fatalf("failed to parse deltaLine: %v", err)
	}

	if sl.Type != "stream_event" {
		t.Errorf("expected type 'stream_event', got %q", sl.Type)
	}
	if sl.Event == nil || sl.Event.Type != "content_block_delta" {
		t.Errorf("expected event type 'content_block_delta'")
	}
	if sl.Event.Delta == nil || sl.Event.Delta.Text != "Hello" {
		t.Errorf("expected delta text 'Hello', got %v", sl.Event.Delta)
	}

	resultLine := `{"type":"result","subtype":"success","result":"Done!","usage":{"input_tokens":10,"output_tokens":20}}`
	var slResult StreamLine
	if err := json.Unmarshal([]byte(resultLine), &slResult); err != nil {
		t.Fatalf("failed to parse resultLine: %v", err)
	}

	if slResult.Type != "result" {
		t.Errorf("expected type 'result', got %q", slResult.Type)
	}
	if slResult.Result != "Done!" {
		t.Errorf("expected result 'Done!', got %q", slResult.Result)
	}
	if slResult.Usage == nil || slResult.Usage.InputTokens != 10 || slResult.Usage.OutputTokens != 20 {
		t.Errorf("invalid usage stats: %v", slResult.Usage)
	}
}
