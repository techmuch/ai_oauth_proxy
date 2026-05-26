package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai_oauth_proxy/metrics"
)

func TestHandleModels(t *testing.T) {
	tracker := metrics.NewTokenTracker()
	srv := NewServer(18081, tracker)

	req, err := http.NewRequest("GET", "/v1/models", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(srv.handleModels)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status OK, got %v", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}

	if resp["object"] != "list" {
		t.Errorf("expected object to be 'list', got %v", resp["object"])
	}

	data, ok := resp["data"].([]interface{})
	if !ok || len(data) == 0 {
		t.Fatalf("expected non-empty data slice")
	}

	firstModel, ok := data[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected map structure for model")
	}

	if firstModel["id"] != "claude-sonnet-4-6" {
		t.Errorf("expected first model id to be 'claude-sonnet-4-6', got %v", firstModel["id"])
	}
}

func TestCORSHeaders(t *testing.T) {
	tracker := metrics.NewTokenTracker()
	srv := NewServer(18081, tracker)

	handler := srv.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req, err := http.NewRequest("OPTIONS", "/v1/chat/completions", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected OPTIONS status OK, got %v", rr.Code)
	}

	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected Access-Control-Allow-Origin header to be '*', got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}
