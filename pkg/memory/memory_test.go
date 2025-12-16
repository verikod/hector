// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel
//
// Licensed under the GNU Affero General Public License v3.0 (AGPL-3.0) (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.gnu.org/licenses/agpl-3.0.en.html
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memory_test

import (
	"context"
	"iter"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/memory"
)

// =============================================================================
// In-Memory Service Tests
// =============================================================================

func TestInMemoryService_AddAndSearch(t *testing.T) {
	tests := []struct {
		name         string
		sessions     []testSession
		req          *memory.SearchRequest
		wantCount    int
		wantContains []string
	}{
		{
			name: "find matching events",
			sessions: []testSession{
				makeTestSession("app1", "user1", "sess1", []testEvent{
					{Author: "user", Text: "The quick brown fox", Timestamp: time.Now()},
					{Author: "assistant", Text: "jumps over the lazy dog", Timestamp: time.Now()},
				}),
			},
			req: &memory.SearchRequest{
				AppName: "app1",
				UserID:  "user1",
				Query:   "quick fox",
			},
			wantCount:    1,
			wantContains: []string{"quick brown fox"},
		},
		{
			name: "search across multiple sessions",
			sessions: []testSession{
				makeTestSession("app1", "user1", "sess1", []testEvent{
					{Author: "user", Text: "hello world", Timestamp: time.Now()},
				}),
				makeTestSession("app1", "user1", "sess2", []testEvent{
					{Author: "assistant", Text: "hello there", Timestamp: time.Now()},
				}),
			},
			req: &memory.SearchRequest{
				AppName: "app1",
				UserID:  "user1",
				Query:   "hello",
			},
			wantCount:    2,
			wantContains: []string{"hello world", "hello there"},
		},
		{
			name: "no leakage for different app",
			sessions: []testSession{
				makeTestSession("app1", "user1", "sess1", []testEvent{
					{Author: "user", Text: "secret data", Timestamp: time.Now()},
				}),
			},
			req: &memory.SearchRequest{
				AppName: "app2", // Different app
				UserID:  "user1",
				Query:   "secret",
			},
			wantCount: 0,
		},
		{
			name: "no leakage for different user",
			sessions: []testSession{
				makeTestSession("app1", "user1", "sess1", []testEvent{
					{Author: "user", Text: "private info", Timestamp: time.Now()},
				}),
			},
			req: &memory.SearchRequest{
				AppName: "app1",
				UserID:  "user2", // Different user
				Query:   "private",
			},
			wantCount: 0,
		},
		{
			name: "no matches for unrelated query",
			sessions: []testSession{
				makeTestSession("app1", "user1", "sess1", []testEvent{
					{Author: "user", Text: "hello world", Timestamp: time.Now()},
				}),
			},
			req: &memory.SearchRequest{
				AppName: "app1",
				UserID:  "user1",
				Query:   "goodbye universe",
			},
			wantCount: 0,
		},
		{
			name: "empty query returns no results",
			sessions: []testSession{
				makeTestSession("app1", "user1", "sess1", []testEvent{
					{Author: "user", Text: "some content", Timestamp: time.Now()},
				}),
			},
			req: &memory.SearchRequest{
				AppName: "app1",
				UserID:  "user1",
				Query:   "",
			},
			wantCount: 0,
		},
		{
			name: "search on empty store",
			req: &memory.SearchRequest{
				AppName: "app1",
				UserID:  "user1",
				Query:   "anything",
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := memory.NewKeywordIndexService()
			ctx := context.Background()

			// Index sessions
			for _, sess := range tt.sessions {
				if err := svc.Index(ctx, &sess); err != nil {
					t.Fatalf("Index failed: %v", err)
				}
			}

			// Search
			resp, err := svc.Search(ctx, tt.req)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}

			// Check count
			if len(resp.Results) != tt.wantCount {
				t.Errorf("got %d results, want %d", len(resp.Results), tt.wantCount)
			}

			// Check content
			for _, want := range tt.wantContains {
				found := false
				for _, r := range resp.Results {
					if containsSubstring(r.Content, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected result containing %q", want)
				}
			}
		})
	}
}

func TestKeywordIndexService_SessionUpdate(t *testing.T) {
	svc := memory.NewKeywordIndexService()
	ctx := context.Background()

	// Index initial session
	sess1 := makeTestSession("app1", "user1", "sess1", []testEvent{
		{Author: "user", Text: "initial message", Timestamp: time.Now()},
	})
	if err := svc.Index(ctx, &sess1); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Search should find initial message
	resp, _ := svc.Search(ctx, &memory.SearchRequest{
		AppName: "app1", UserID: "user1", Query: "initial",
	})
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}

	// Update session with new events (simulating re-ingestion)
	sess2 := makeTestSession("app1", "user1", "sess1", []testEvent{
		{Author: "user", Text: "initial message", Timestamp: time.Now()},
		{Author: "assistant", Text: "updated response", Timestamp: time.Now()},
	})
	if err := svc.Index(ctx, &sess2); err != nil {
		t.Fatalf("Index (update) failed: %v", err)
	}

	// Search should find updated content
	resp, _ = svc.Search(ctx, &memory.SearchRequest{
		AppName: "app1", UserID: "user1", Query: "updated",
	})
	if len(resp.Results) != 1 {
		t.Errorf("expected 1 result for 'updated', got %d", len(resp.Results))
	}
}

func TestKeywordIndexService_Score(t *testing.T) {
	svc := memory.NewKeywordIndexService()
	ctx := context.Background()

	sess := makeTestSession("app1", "user1", "sess1", []testEvent{
		{Author: "user", Text: "apple banana cherry", Timestamp: time.Now()},
		{Author: "assistant", Text: "apple apple apple", Timestamp: time.Now()},
	})
	if err := svc.Index(ctx, &sess); err != nil {
		t.Fatalf("AddSession failed: %v", err)
	}

	resp, _ := svc.Search(ctx, &memory.SearchRequest{
		AppName: "app1", UserID: "user1", Query: "apple",
	})

	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}

	// Results should have positive scores
	for _, r := range resp.Results {
		if r.Score <= 0 {
			t.Errorf("expected positive score, got %f", r.Score)
		}
	}
}

// =============================================================================
// Adapter Tests
// =============================================================================

func TestAdapter_Search(t *testing.T) {
	svc := memory.NewKeywordIndexService()
	ctx := context.Background()

	// Index a session
	sess := makeTestSession("app1", "user1", "sess1", []testEvent{
		{Author: "user", Text: "favorite color is blue", Timestamp: time.Now()},
	})
	if err := svc.Index(ctx, &sess); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Create adapter with correct scope
	adapter := memory.NewAdapter(svc, "app1", "user1")

	// Search through adapter
	resp, err := adapter.Search(ctx, "favorite color")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}

	// Check metadata enrichment
	result := resp.Results[0]
	if result.Metadata["session_id"] != "sess1" {
		t.Errorf("expected session_id in metadata")
	}
	if result.Metadata["author"] != "user" {
		t.Errorf("expected author in metadata")
	}
}

func TestAdapter_ScopedSearch(t *testing.T) {
	svc := memory.NewKeywordIndexService()
	ctx := context.Background()

	// Index sessions for different users
	sess1 := makeTestSession("app1", "user1", "sess1", []testEvent{
		{Author: "user", Text: "user1 secret", Timestamp: time.Now()},
	})
	sess2 := makeTestSession("app1", "user2", "sess2", []testEvent{
		{Author: "user", Text: "user2 secret", Timestamp: time.Now()},
	})
	_ = svc.Index(ctx, &sess1)
	_ = svc.Index(ctx, &sess2)

	// Adapter for user1 should only see user1's data
	adapter1 := memory.NewAdapter(svc, "app1", "user1")
	resp1, _ := adapter1.Search(ctx, "secret")
	if len(resp1.Results) != 1 {
		t.Errorf("user1 adapter should see 1 result, got %d", len(resp1.Results))
	}
	if !containsSubstring(resp1.Results[0].Content, "user1") {
		t.Errorf("user1 adapter should only see user1's data")
	}

	// Adapter for user2 should only see user2's data
	adapter2 := memory.NewAdapter(svc, "app1", "user2")
	resp2, _ := adapter2.Search(ctx, "secret")
	if len(resp2.Results) != 1 {
		t.Errorf("user2 adapter should see 1 result, got %d", len(resp2.Results))
	}
	if !containsSubstring(resp2.Results[0].Content, "user2") {
		t.Errorf("user2 adapter should only see user2's data")
	}
}

func TestAdapter_NilService(t *testing.T) {
	// Adapter should handle nil service gracefully
	adapter := memory.NewAdapter(nil, "app1", "user1")

	resp, err := adapter.Search(context.Background(), "anything")
	if err != nil {
		t.Errorf("nil service should not error: %v", err)
	}
	if resp == nil {
		t.Error("response should not be nil")
		return
	}
	if len(resp.Results) != 0 {
		t.Errorf("nil service should return empty results")
	}
}

// =============================================================================
// NilMemory Tests
// =============================================================================

func TestNilMemory(t *testing.T) {
	mem := memory.NilMemory()
	ctx := context.Background()

	// AddSession should not error
	sess := makeTestSession("app1", "user1", "sess1", []testEvent{
		{Author: "user", Text: "test", Timestamp: time.Now()},
	})
	if err := mem.AddSession(ctx, &sess); err != nil {
		t.Errorf("NilMemory.AddSession should not error: %v", err)
	}

	// Search should return empty results
	resp, err := mem.Search(ctx, "anything")
	if err != nil {
		t.Errorf("NilMemory.Search should not error: %v", err)
	}
	if resp == nil {
		t.Error("response should not be nil")
		return
	}
	if len(resp.Results) != 0 {
		t.Error("NilMemory should always return empty results")
	}
}

func TestNilMemory_ImplementsInterface(t *testing.T) {
	var _ agent.Memory = memory.NilMemory()
}

// =============================================================================
// IndexService Interface Tests
// =============================================================================

func TestKeywordIndexService_ImplementsInterface(t *testing.T) {
	var _ memory.IndexService = memory.NewKeywordIndexService()
}

func TestAdapter_ImplementsInterface(t *testing.T) {
	svc := memory.NewKeywordIndexService()
	var _ agent.Memory = memory.NewAdapter(svc, "app", "user")
}

// =============================================================================
// Test Helpers
// =============================================================================

type testEvent struct {
	Author    string
	Text      string
	Timestamp time.Time
}

type testSession struct {
	appName   string
	userID    string
	sessionID string
	events    []testEvent
}

func makeTestSession(appName, userID, sessionID string, events []testEvent) testSession {
	return testSession{
		appName:   appName,
		userID:    userID,
		sessionID: sessionID,
		events:    events,
	}
}

func (s *testSession) ID() string      { return s.sessionID }
func (s *testSession) AppName() string { return s.appName }
func (s *testSession) UserID() string  { return s.userID }
func (s *testSession) State() agent.State {
	return nil // Not needed for memory tests
}
func (s *testSession) Events() agent.Events {
	return &testEvents{events: s.events, sessionID: s.sessionID}
}

type testEvents struct {
	events    []testEvent
	sessionID string
}

func (e *testEvents) All() iter.Seq[*agent.Event] {
	return func(yield func(*agent.Event) bool) {
		for i, ev := range e.events {
			event := &agent.Event{
				ID:        e.sessionID + "-" + string(rune('0'+i)),
				Author:    ev.Author,
				Timestamp: ev.Timestamp,
				Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: ev.Text}),
			}
			if !yield(event) {
				return
			}
		}
	}
}

func (e *testEvents) Len() int {
	return len(e.events)
}

func (e *testEvents) At(i int) *agent.Event {
	if i < 0 || i >= len(e.events) {
		return nil
	}
	ev := e.events[i]
	return &agent.Event{
		ID:        e.sessionID + "-" + string(rune('0'+i)),
		Author:    ev.Author,
		Timestamp: ev.Timestamp,
		Message:   a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: ev.Text}),
	}
}

func containsSubstring(s, substr string) bool {
	return slices.Contains([]rune(s), []rune(substr)[0]) || findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// =============================================================================
// Working Memory Strategy Tests
// =============================================================================

func TestNilWorkingMemory(t *testing.T) {
	strategy := memory.NilWorkingMemory{}

	// Name
	if strategy.Name() != "none" {
		t.Errorf("expected name 'none', got %q", strategy.Name())
	}

	// FilterEvents should return all events unchanged
	events := []*agent.Event{
		{ID: "1", Author: "user"},
		{ID: "2", Author: "assistant"},
		{ID: "3", Author: "user"},
	}
	filtered := strategy.FilterEvents(events)
	if len(filtered) != len(events) {
		t.Errorf("NilWorkingMemory should return all events, got %d", len(filtered))
	}

	// CheckAndSummarize should return nil
	summary, err := strategy.CheckAndSummarize(context.Background(), events)
	if err != nil {
		t.Errorf("CheckAndSummarize should not error: %v", err)
	}
	if summary != nil {
		t.Error("NilWorkingMemory should not create summary")
	}
}

func TestBufferWindowStrategy(t *testing.T) {
	tests := []struct {
		name       string
		windowSize int
		numEvents  int
		wantKept   int
	}{
		{"keep all when under window", 10, 5, 5},
		{"keep all when at window", 10, 10, 10},
		{"truncate when over window", 10, 15, 10},
		{"small window", 3, 10, 3},
		{"empty events", 10, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := memory.NewBufferWindowStrategy(memory.BufferWindowConfig{
				WindowSize: tt.windowSize,
			})

			if strategy.Name() != "buffer_window" {
				t.Errorf("expected name 'buffer_window', got %q", strategy.Name())
			}

			// Create test events
			events := make([]*agent.Event, tt.numEvents)
			for i := 0; i < tt.numEvents; i++ {
				events[i] = &agent.Event{ID: string(rune('0' + i)), Author: "user"}
			}

			filtered := strategy.FilterEvents(events)
			if len(filtered) != tt.wantKept {
				t.Errorf("expected %d events, got %d", tt.wantKept, len(filtered))
			}

			// Verify we kept the LAST events
			if tt.numEvents > tt.windowSize {
				expectedFirstID := string(rune('0' + tt.numEvents - tt.windowSize))
				if len(filtered) > 0 && filtered[0].ID != expectedFirstID {
					t.Errorf("expected first kept event to be %q, got %q", expectedFirstID, filtered[0].ID)
				}
			}

			// CheckAndSummarize should always return nil (no summarization)
			summary, err := strategy.CheckAndSummarize(context.Background(), events)
			if err != nil {
				t.Errorf("CheckAndSummarize should not error: %v", err)
			}
			if summary != nil {
				t.Error("BufferWindow should not create summary")
			}
		})
	}
}

func TestBufferWindowStrategy_DefaultSize(t *testing.T) {
	// Test that default window size is applied
	strategy := memory.NewBufferWindowStrategy(memory.BufferWindowConfig{})
	if strategy.WindowSize() != memory.DefaultBufferWindowSize {
		t.Errorf("expected default window size %d, got %d",
			memory.DefaultBufferWindowSize, strategy.WindowSize())
	}
}

func TestWorkingMemoryProvider_Interface(t *testing.T) {
	// Verify the interface is exported and usable
	var _ memory.WorkingMemoryProvider = &mockWorkingMemoryProvider{}
}

// mockWorkingMemoryProvider for interface testing
type mockWorkingMemoryProvider struct{}

func (m *mockWorkingMemoryProvider) WorkingMemory() memory.WorkingMemoryStrategy {
	return memory.NilWorkingMemory{}
}
