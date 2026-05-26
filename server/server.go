package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"ai_oauth_proxy/engine"
	"ai_oauth_proxy/metrics"
)

type Server struct {
	tracker *metrics.TokenTracker
	port    int
}

type ChatCompletionsRequest struct {
	Model    string           `json:"model"`
	Messages []engine.Message `json:"messages"`
	Stream   bool             `json:"stream"`
}

type ChatCompletionsResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int            `json:"index"`
	Message      engine.Message `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

type ChunkChoice struct {
	Index        int            `json:"index"`
	Delta        ChunkDelta     `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type ChunkDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

func NewServer(port int, tracker *metrics.TokenTracker) *Server {
	return &Server{
		port:    port,
		tracker: tracker,
	}
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("chatcmpl-%x", b)
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)

	// Wrap with logging and CORS middleware
	handler := s.corsMiddleware(s.logMiddleware(mux))

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Starting Custom AI Proxy Server on http://localhost%s", addr)
	return http.ListenAndServe(addr, handler)
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, DELETE, PUT")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Strip Bearer Authorization header from logs for security
		authHeader := r.Header.Get("Authorization")
		hasAuth := authHeader != ""

		log.Printf("Incoming request: %s %s (Authenticated: %t)", r.Method, r.URL.Path, hasAuth)
		next.ServeHTTP(w, r)
		log.Printf("Completed request: %s %s in %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	modelsResponse := map[string]interface{}{
		"object": "list",
		"data": []map[string]interface{}{
			{
				"id":       "claude-sonnet-4-6",
				"object":   "model",
				"created":  1700000000,
				"owned_by": "anthropic",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(modelsResponse)
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read and parse the OpenAI request body
	var req ChatCompletionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Bad request: %v", err), http.StatusBadRequest)
		return
	}

	// Normalize payload & flatten conversation history
	systemPrompt, transcript := engine.FlattenMessages(req.Messages)

	// Spawn subprocess for Claude Code CLI using request context
	eventChan, err := engine.RunClaude(r.Context(), systemPrompt, transcript)
	if err != nil {
		log.Printf("Error spawning Claude subprocess: %v", err)
		http.Error(w, fmt.Sprintf("Internal Server Error: %v", err), http.StatusInternalServerError)
		return
	}

	// Setup ID and timestamp
	id := generateID()
	created := time.Now().Unix()

	if req.Stream {
		// Stream Server-Sent Events (SSE)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // Disable proxy buffering for nginx

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Send initial role delta
		initialChunk := ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   "claude-sonnet-4-6",
			Choices: []ChunkChoice{
				{
					Index: 0,
					Delta: ChunkDelta{
						Role: "assistant",
					},
				},
			},
		}
		initialBytes, _ := json.Marshal(initialChunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(initialBytes))
		flusher.Flush()

		// Read and forward SSE chunks
		for event := range eventChan {
			switch event.Type {
			case "delta":
				chunk := ChatCompletionChunk{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   "claude-sonnet-4-6",
					Choices: []ChunkChoice{
						{
							Index: 0,
							Delta: ChunkDelta{
								Content: event.Text,
							},
						},
					},
				}
				chunkBytes, _ := json.Marshal(chunk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(chunkBytes))
				flusher.Flush()

			case "usage":
				// Update thread-safe token tracker
				s.tracker.AddUsage(event.InputTokens, event.OutputTokens, event.CacheRead, event.CacheCreate)

			case "error":
				log.Printf("Error from Claude runner: %v", event.Error)
				// Forward error information inside the stream
				errChunk := ChatCompletionChunk{
					ID:      id,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   "claude-sonnet-4-6",
					Choices: []ChunkChoice{
						{
							Index: 0,
							Delta: ChunkDelta{
								Content: fmt.Sprintf("\n[Proxy Error: %v]", event.Error),
							},
						},
					},
				}
				errBytes, _ := json.Marshal(errChunk)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(errBytes))
				flusher.Flush()
			}
		}

		// Send final stop choice
		stopReason := "stop"
		finalChunk := ChatCompletionChunk{
			ID:      id,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   "claude-sonnet-4-6",
			Choices: []ChunkChoice{
				{
					Index:        0,
					FinishReason: &stopReason,
				},
			},
		}
		finalBytes, _ := json.Marshal(finalChunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(finalBytes))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()

	} else {
		// Non-streaming response: accumulate whole output
		var fullText string
		var inputTokens, outputTokens, cacheRead, cacheCreate int64

		for event := range eventChan {
			switch event.Type {
			case "delta":
				fullText += event.Text
			case "result":
				// The engine sends full text at result too, but delta builds it. Let's make sure we have the complete output.
				if event.Text != "" {
					fullText = event.Text
				}
			case "usage":
				inputTokens = event.InputTokens
				outputTokens = event.OutputTokens
				cacheRead = event.CacheRead
				cacheCreate = event.CacheCreate
				s.tracker.AddUsage(inputTokens, outputTokens, cacheRead, cacheCreate)
			case "error":
				log.Printf("Error from Claude runner: %v", event.Error)
				http.Error(w, fmt.Sprintf("Engine error: %v", event.Error), http.StatusInternalServerError)
				return
			}
		}

		resp := ChatCompletionsResponse{
			ID:      id,
			Object:  "chat.completion",
			Created: created,
			Model:   "claude-sonnet-4-6",
			Choices: []Choice{
				{
					Index: 0,
					Message: engine.Message{
						Role:    "assistant",
						Content: fullText,
					},
					FinishReason: "stop",
				},
			},
			Usage: Usage{
				PromptTokens:     inputTokens,
				CompletionTokens: outputTokens,
				TotalTokens:      inputTokens + outputTokens,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}
