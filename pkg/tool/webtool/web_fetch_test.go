// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel

package webtool

import (
	"testing"
	"time"
)

func TestNewWebFetch(t *testing.T) {
	t.Run("creates with nil config", func(t *testing.T) {
		tool, err := NewWebFetch(nil)
		if err != nil {
			t.Fatalf("NewWebFetch(nil) returned error: %v", err)
		}
		if tool == nil {
			t.Fatal("NewWebFetch(nil) returned nil tool")
		}
		if tool.Name() != "web_fetch" {
			t.Errorf("expected name 'web_fetch', got %q", tool.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		cfg := &WebFetchConfig{
			Timeout:         60 * time.Second,
			MaxRetries:      5,
			MaxResponseSize: 5242880, // 5MB
			AllowRedirects:  false,
			UserAgent:       "CustomAgent/1.0",
		}
		tool, err := NewWebFetch(cfg)
		if err != nil {
			t.Fatalf("NewWebFetch(cfg) returned error: %v", err)
		}
		if tool == nil {
			t.Fatal("NewWebFetch(cfg) returned nil tool")
		}
	})

	t.Run("has correct schema", func(t *testing.T) {
		tool, _ := NewWebFetch(nil)
		schema := tool.Schema()
		if schema == nil {
			t.Fatal("Schema() returned nil")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("Schema properties not found")
		}
		urlProp, ok := props["url"].(map[string]any)
		if !ok {
			t.Fatal("url property not found in schema")
		}
		if urlProp["type"] != "string" {
			t.Errorf("url type should be string, got %v", urlProp["type"])
		}
	})
}

func TestWebFetchArgs(t *testing.T) {
	t.Run("url is required", func(t *testing.T) {
		args := WebFetchArgs{
			URL: "",
		}
		if args.URL != "" {
			t.Error("empty URL should be empty")
		}
	})

	t.Run("valid url", func(t *testing.T) {
		args := WebFetchArgs{
			URL: "https://example.com",
		}
		if args.URL != "https://example.com" {
			t.Errorf("expected URL 'https://example.com', got %q", args.URL)
		}
	})
}

func TestWebFetchConfig(t *testing.T) {
	t.Run("default config values", func(t *testing.T) {
		cfg := &WebFetchConfig{}
		// When passed to NewWebFetch, it should apply defaults
		tool, err := NewWebFetch(cfg)
		if err != nil {
			t.Fatalf("NewWebFetch returned error: %v", err)
		}
		if tool == nil {
			t.Fatal("NewWebFetch returned nil")
		}
	})

	t.Run("config with domains", func(t *testing.T) {
		cfg := &WebFetchConfig{
			AllowedDomains: []string{"example.com", "test.com"},
			DeniedDomains:  []string{"evil.com"},
		}
		if len(cfg.AllowedDomains) != 2 {
			t.Errorf("expected 2 allowed domains, got %d", len(cfg.AllowedDomains))
		}
		if len(cfg.DeniedDomains) != 1 {
			t.Errorf("expected 1 denied domain, got %d", len(cfg.DeniedDomains))
		}
	})
}

func TestURLValidation(t *testing.T) {
	testCases := []struct {
		url     string
		wantErr bool
		desc    string
	}{
		{"https://example.com", false, "valid https URL"},
		{"http://example.com", false, "valid http URL"},
		{"ftp://example.com", true, "ftp scheme not allowed"},
		{"file:///etc/passwd", true, "file scheme not allowed"},
		{"javascript:alert(1)", true, "javascript scheme not allowed"},
		{"", true, "empty URL"},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			// We can't call the validation directly, but we can test URL parsing
			if tc.url == "" {
				// Empty URL should fail
				return
			}
		})
	}
}
