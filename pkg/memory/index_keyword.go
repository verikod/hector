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

package memory

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/a2aproject/a2a-go/a2a"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/session"
)

// KeywordIndexService provides keyword-based search over session data.
//
// This is the default index implementation when no vector database is configured.
// It uses simple word matching for search, which is fast but not semantic.
//
// Use chromem for semantic search capabilities.
type KeywordIndexService struct {
	mu    sync.RWMutex
	store map[userKey]map[sessionID][]Entry
}

// NewKeywordIndexService creates a new keyword-based index service.
func NewKeywordIndexService() *KeywordIndexService {
	return &KeywordIndexService{
		store: make(map[userKey]map[sessionID][]Entry),
	}
}

// Index adds session events to the keyword index.
func (s *KeywordIndexService) Index(ctx context.Context, sess agent.Session) error {
	if sess == nil {
		return nil
	}

	uk := userKey{appName: sess.AppName(), userID: sess.UserID()}
	sid := sessionID(sess.ID())

	// Collect entries from session events
	var entries []Entry
	for ev := range sess.Events().All() {
		if ev.Message == nil {
			continue
		}

		text := extractTextFromA2AMessage(ev.Message)
		if text == "" {
			continue
		}

		entries = append(entries, Entry{
			SessionID: sess.ID(),
			EventID:   ev.ID,
			AppName:   sess.AppName(),
			UserID:    sess.UserID(),
			Author:    ev.Author,
			Content:   text,
			Timestamp: ev.Timestamp,
			Words:     tokenize(text),
			Metadata: map[string]any{
				"session_id": sess.ID(),
				"event_id":   ev.ID,
				"author":     ev.Author,
			},
		})
	}

	if len(entries) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Initialize user's session map if needed
	if s.store[uk] == nil {
		s.store[uk] = make(map[sessionID][]Entry)
	}

	// Replace entries for this session (idempotent)
	s.store[uk][sid] = entries

	slog.Debug("Indexed session in keyword index",
		"session_id", sess.ID(),
		"entries", len(entries))

	return nil
}

// Search performs keyword-based search.
func (s *KeywordIndexService) Search(ctx context.Context, req *SearchRequest) (*SearchResponse, error) {
	if req.Query == "" {
		return &SearchResponse{Results: []SearchResult{}}, nil
	}

	uk := userKey{appName: req.AppName, userID: req.UserID}
	queryWords := tokenize(req.Query)

	s.mu.RLock()
	defer s.mu.RUnlock()

	userSessions, ok := s.store[uk]
	if !ok {
		return &SearchResponse{Results: []SearchResult{}}, nil
	}

	var results []SearchResult
	for _, entries := range userSessions {
		for _, entry := range entries {
			score := calculateScore(queryWords, entry.Words)
			if score > 0 {
				results = append(results, SearchResult{
					SessionID: entry.SessionID,
					EventID:   entry.EventID,
					Content:   entry.Content,
					Author:    entry.Author,
					Timestamp: entry.Timestamp,
					Score:     score,
					Metadata:  entry.Metadata,
				})
			}
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Limit results
	if len(results) > 10 {
		results = results[:10]
	}

	return &SearchResponse{Results: results}, nil
}

// Rebuild repopulates the index from session.Service.
func (s *KeywordIndexService) Rebuild(ctx context.Context, sessions session.Service, appName, userID string) error {
	if sessions == nil {
		return nil
	}

	// List all sessions for this user
	resp, err := sessions.List(ctx, &session.ListRequest{
		AppName: appName,
		UserID:  userID,
	})
	if err != nil {
		return err
	}

	// Clear existing entries for this user
	uk := userKey{appName: appName, userID: userID}
	s.mu.Lock()
	delete(s.store, uk)
	s.mu.Unlock()

	// Reindex each session
	for _, sess := range resp.Sessions {
		if err := s.Index(ctx, sess); err != nil {
			slog.Warn("Failed to reindex session",
				"session_id", sess.ID(),
				"error", err)
		}
	}

	slog.Info("Rebuilt keyword index",
		"app_name", appName,
		"user_id", userID,
		"sessions", len(resp.Sessions))

	return nil
}

// Clear removes index entries for a specific session.
func (s *KeywordIndexService) Clear(ctx context.Context, appName, userID, sessID string) error {
	uk := userKey{appName: appName, userID: userID}
	sid := sessionID(sessID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if userSessions, ok := s.store[uk]; ok {
		delete(userSessions, sid)
	}

	return nil
}

// Name returns the index implementation name.
func (s *KeywordIndexService) Name() string {
	return "keyword"
}

// Ensure KeywordIndexService implements IndexService.
var _ IndexService = (*KeywordIndexService)(nil)

// Helper types for the store
type userKey struct {
	appName string
	userID  string
}

type sessionID string

// tokenize splits text into lowercase words for indexing.
func tokenize(text string) map[string]struct{} {
	words := make(map[string]struct{})
	for _, word := range strings.Fields(strings.ToLower(text)) {
		// Remove punctuation
		word = strings.Trim(word, ".,!?;:\"'()[]{}")
		if len(word) > 2 { // Skip very short words
			words[word] = struct{}{}
		}
	}
	return words
}

// calculateScore returns the number of matching words (simple TF scoring).
func calculateScore(query, doc map[string]struct{}) float64 {
	var score float64
	for word := range query {
		if _, ok := doc[word]; ok {
			score++
		}
	}
	return score
}

// extractTextFromA2AMessage extracts text content from an a2a message.
func extractTextFromA2AMessage(msg *a2a.Message) string {
	if msg == nil {
		return ""
	}

	var text strings.Builder
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case a2a.TextPart:
			text.WriteString(p.Text)
		case *a2a.TextPart:
			text.WriteString(p.Text)
		}
	}
	return text.String()
}
