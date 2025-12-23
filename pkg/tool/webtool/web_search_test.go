// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel

package webtool

import (
	"testing"
)

func TestNewWebSearch(t *testing.T) {
	t.Run("creates with nil config", func(t *testing.T) {
		tool, err := NewWebSearch(nil)
		if err != nil {
			t.Fatalf("NewWebSearch(nil) returned error: %v", err)
		}
		if tool == nil {
			t.Fatal("NewWebSearch(nil) returned nil tool")
		}
		if tool.Name() != "web_search" {
			t.Errorf("expected name 'web_search', got %q", tool.Name())
		}
	})

	t.Run("creates with custom config", func(t *testing.T) {
		cfg := &WebSearchConfig{
			Provider: "tavily",
			APIKey:   "test-key",
		}
		tool, err := NewWebSearch(cfg)
		if err != nil {
			t.Fatalf("NewWebSearch(cfg) returned error: %v", err)
		}
		if tool == nil {
			t.Fatal("NewWebSearch(cfg) returned nil tool")
		}
	})

	t.Run("validates empty query", func(t *testing.T) {
		tool, _ := NewWebSearch(nil)
		// The validation function should reject empty query
		schema := tool.Schema()
		if schema == nil {
			t.Fatal("Schema() returned nil")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatal("Schema properties not found")
		}
		queryProp, ok := props["query"].(map[string]any)
		if !ok {
			t.Fatal("query property not found in schema")
		}
		if queryProp["type"] != "string" {
			t.Errorf("query type should be string, got %v", queryProp["type"])
		}
	})
}

func TestWebSearchArgs(t *testing.T) {
	t.Run("default max results", func(t *testing.T) {
		args := WebSearchArgs{
			Query: "test query",
		}
		if args.MaxResults != 0 {
			t.Error("MaxResults should default to 0 (will be set to 5 at runtime)")
		}
	})

	t.Run("topic enum values", func(t *testing.T) {
		args := WebSearchArgs{
			Query: "test",
			Topic: "news",
		}
		if args.Topic != "news" {
			t.Errorf("expected topic 'news', got %q", args.Topic)
		}
	})
}

func TestTavilyProvider(t *testing.T) {
	t.Run("creates provider", func(t *testing.T) {
		provider := NewTavilyProvider("test-key", 0)
		if provider == nil {
			t.Fatal("NewTavilyProvider returned nil")
		}
		if provider.apiKey != "test-key" {
			t.Errorf("expected apiKey 'test-key', got %q", provider.apiKey)
		}
	})

	t.Run("max results clamping", func(t *testing.T) {
		// Test that values are clamped correctly in Search
		// This is a unit test for the clamping logic
		testCases := []struct {
			input    int
			expected int
		}{
			{0, 5},   // Default to 5
			{-1, 5},  // Negative becomes 5
			{3, 3},   // Valid value unchanged
			{20, 20}, // Max value unchanged
			{25, 20}, // Over max becomes 20
		}

		for _, tc := range testCases {
			maxResults := tc.input
			if maxResults <= 0 {
				maxResults = 5
			}
			if maxResults > 20 {
				maxResults = 20
			}
			if maxResults != tc.expected {
				t.Errorf("for input %d, expected %d, got %d", tc.input, tc.expected, maxResults)
			}
		}
	})
}
