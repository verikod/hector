package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/verikod/hector/pkg/auth"
	"github.com/verikod/hector/pkg/config"
)

// mockValidator implements auth.TokenValidator
type mockValidator struct {
	validToken string
}

func (m *mockValidator) ValidateToken(ctx context.Context, token string) (*auth.Claims, error) {
	if token == m.validToken {
		return &auth.Claims{Subject: "user"}, nil
	}
	return nil, fmt.Errorf("invalid token")
}

func (m *mockValidator) Close() error { return nil }

func TestAgentVisibility(t *testing.T) {
	// Setup config with mixed visibility
	cfg := &config.Config{
		Agents: map[string]*config.AgentConfig{
			"pub":  {Visibility: "public", Name: "pub"},
			"int":  {Visibility: "internal", Name: "int"},
			"priv": {Visibility: "private", Name: "priv"},
		},
		Server: config.ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
			Auth: &config.AuthConfig{
				Enabled:  true,
				JWKSURL:  "https://dummy",
				Issuer:   "dummy",
				Audience: "dummy",
			},
		},
	}

	// Setup executors (required for handlers to be built)
	executors := map[string]*Executor{
		"pub":  {},
		"int":  {},
		"priv": {},
	}

	// Setup server
	validator := &mockValidator{validToken: "valid"}
	srv := NewHTTPServer(cfg, executors, WithAuthValidator(validator))
	handler := srv.setupRoutes()

	// Helper to make request
	checkRequest := func(t *testing.T, method, path, token string, expectedCode int) []byte {
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != expectedCode {
			t.Errorf("%s %s (Token: %s): Expected status %d, got %d", method, path, token, expectedCode, w.Code)
		}
		return w.Body.Bytes()
	}

	// 1. Discovery Logic
	t.Run("Discovery", func(t *testing.T) {
		// Case A: No Auth - Should see PUBLIC only
		body := checkRequest(t, "GET", "/agents", "", 200)
		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}
		agents := resp["agents"].([]interface{})
		found := make(map[string]bool)
		for _, a := range agents {
			card := a.(map[string]interface{})
			found[card["name"].(string)] = true
		}

		if !found["pub"] {
			t.Error("Public agent missing from public discovery")
		}
		if found["int"] {
			t.Error("Internal agent leaked in public discovery")
		}
		if found["priv"] {
			t.Error("Private agent leaked in public discovery")
		}

		// Case B: With Auth - Should see PUBLIC and INTERNAL
		body = checkRequest(t, "GET", "/agents", "valid", 200)
		_ = json.Unmarshal(body, &resp)
		agents = resp["agents"].([]interface{})
		found = make(map[string]bool)
		for _, a := range agents {
			card := a.(map[string]interface{})
			found[card["name"].(string)] = true
		}

		if !found["pub"] {
			t.Error("Public agent missing from auth discovery")
		}
		if !found["int"] {
			t.Error("Internal agent missing from auth discovery")
		}
		if found["priv"] {
			t.Error("Private agent leaked in auth discovery")
		}
	})

	// 2. Direct Access Logic
	t.Run("DirectAccess", func(t *testing.T) {
		// Case A: Public Agent - Accessible without auth
		checkRequest(t, "GET", "/agents/pub", "", 200)

		// Case B: Internal Agent - Blocked without auth
		checkRequest(t, "GET", "/agents/int", "", 401)

		// Case C: Internal Agent - Accessible with auth
		checkRequest(t, "GET", "/agents/int", "valid", 200)

		// Case D: Private Agent - Blocked always (404/403)
		// We verify it returns 404 as if it doesn't exist
		checkRequest(t, "GET", "/agents/priv", "valid", 404)
	})
}
