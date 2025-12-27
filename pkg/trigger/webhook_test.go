package trigger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/verikod/hector/pkg/config"
)

func TestWebhookHandler_Callback(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *config.TriggerConfig
		payload        string
		resultProvider func(ctx context.Context, taskID string) (string, error)
		expectStatus   int
		expectCallback bool
		expectResult   string
	}{
		{
			name: "Missing Callback URL",
			cfg: func() *config.TriggerConfig {
				c := &config.TriggerConfig{Type: config.TriggerTypeWebhook}
				c.SetDefaults()
				c.Response.Mode = config.WebhookResponseCallback
				// No callback URL configured
				return c
			}(),
			payload:      `{"input": "hello"}`,
			expectStatus: http.StatusBadRequest,
		},
		{
			name: "Static Callback URL Works",
			cfg: func() *config.TriggerConfig {
				c := &config.TriggerConfig{Type: config.TriggerTypeWebhook}
				c.SetDefaults()
				c.Response.Mode = config.WebhookResponseCallback
				c.Response.CallbackURL = "http://example.com/static"
				return c
			}(),
			payload:        `{"input": "hello"}`,
			expectStatus:   http.StatusAccepted,
			expectCallback: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invoker := func(ctx context.Context, agentName, input string) (string, error) {
				return "task-123", nil
			}

			// Mock callback server
			callbackHit := make(chan bool, 1)
			callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callbackHit <- true
				var res WebhookResult
				if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
					t.Errorf("Failed to decode callback body: %v", err)
				}
				if tt.expectResult != "" && res.Result != tt.expectResult {
					t.Errorf("Expected result %q, got %q", tt.expectResult, res.Result)
				}
			}))
			defer callbackServer.Close()

			// Update config to point to mock server for callback
			if tt.expectCallback {
				if tt.cfg.Response.CallbackURL == "http://example.com/static" {
					tt.cfg.Response.CallbackURL = callbackServer.URL
				}
			}

			h, err := NewWebhookHandler("test-agent", tt.cfg, invoker, WithResultProvider(tt.resultProvider))
			if err != nil {
				t.Fatalf("Failed to create handler: %v", err)
			}

			// Mock request
			req := httptest.NewRequest("POST", "/", strings.NewReader(tt.payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.ServeHTTP(w, req)

			if w.Code != tt.expectStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectStatus, w.Code, w.Body.String())
			}

			if tt.expectCallback {
				select {
				case <-callbackHit:
					// Success
				case <-time.After(1 * time.Second):
					t.Errorf("Timeout waiting for callback")
				}
			}
		})
	}
}

func TestWebhookHandler_ResultProvider(t *testing.T) {
	cfg := &config.TriggerConfig{Type: config.TriggerTypeWebhook}
	cfg.SetDefaults()
	cfg.Response.Mode = config.WebhookResponseCallback
	cfg.Response.CallbackURL = "http://example.com"

	resultCallCount := 0
	resultProvider := func(ctx context.Context, taskID string) (string, error) {
		resultCallCount++
		if resultCallCount < 2 {
			return "", nil // Not ready yet
		}
		return "Actual Result Content", nil
	}

	invoker := func(ctx context.Context, agentName, input string) (string, error) {
		return "task-123", nil
	}

	callbackResult := make(chan string, 1)
	callbackServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var res WebhookResult
		if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
			// This might fail if body is empty or invalid, but we should check err
			return
		}
		callbackResult <- res.Result
	}))
	defer callbackServer.Close()
	cfg.Response.CallbackURL = callbackServer.URL

	h, _ := NewWebhookHandler("test", cfg, invoker, WithResultProvider(resultProvider))

	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"input": "foo"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	select {
	case res := <-callbackResult:
		if res != "Actual Result Content" {
			t.Errorf("Expected result 'Actual Result Content', got %q", res)
		}
	case <-time.After(2 * time.Second):
		t.Errorf("Timeout waiting for callback")
	}
}
