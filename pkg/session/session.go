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

// Package session provides session management for Hector v2.
//
// Sessions represent a series of interactions between a user and agents.
// Each session has:
//   - A unique identifier
//   - Associated app and user
//   - State (key-value store with scope prefixes)
//   - Event history
package session

import (
	"context"
	"errors"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/verikod/hector/pkg/agent"
)

// Session represents a conversation session between user and agents.
type Session interface {
	// ID returns the unique session identifier.
	ID() string

	// AppName returns the application name.
	AppName() string

	// UserID returns the user identifier.
	UserID() string

	// State returns the session state store.
	State() agent.State

	// Events returns the session event history.
	Events() agent.Events

	// LastUpdateTime returns when the session was last modified.
	LastUpdateTime() time.Time
}

// Service manages session lifecycle and persistence.
type Service interface {
	// Get retrieves an existing session.
	Get(ctx context.Context, req *GetRequest) (*GetResponse, error)

	// Create creates a new session.
	Create(ctx context.Context, req *CreateRequest) (*CreateResponse, error)

	// AppendEvent adds an event to the session history.
	AppendEvent(ctx context.Context, session Session, event *agent.Event) error

	// List returns sessions matching the filter criteria.
	List(ctx context.Context, req *ListRequest) (*ListResponse, error)

	// Delete removes a session.
	Delete(ctx context.Context, req *DeleteRequest) error
}

// GetRequest contains parameters for retrieving a session.
type GetRequest struct {
	AppName   string
	UserID    string
	SessionID string

	// NumRecentEvents returns at most N most recent events.
	// Optional: if zero, returns all events.
	NumRecentEvents int

	// After returns events with timestamp >= the given time.
	// Optional: if zero, the filter is not applied.
	After time.Time
}

// GetResponse contains the retrieved session.
type GetResponse struct {
	Session Session
}

// CreateRequest contains parameters for creating a session.
type CreateRequest struct {
	AppName   string
	UserID    string
	SessionID string // Optional - generated if empty
	State     map[string]any
}

// CreateResponse contains the created session.
type CreateResponse struct {
	Session Session
}

// ListRequest contains parameters for listing sessions.
type ListRequest struct {
	AppName   string
	UserID    string
	PageSize  int
	PageToken string
}

// ListResponse contains the list of sessions.
type ListResponse struct {
	Sessions      []Session
	NextPageToken string
}

// DeleteRequest contains parameters for deleting a session.
type DeleteRequest struct {
	AppName   string
	UserID    string
	SessionID string
}

// State prefixes for scoping state keys.
const (
	// KeyPrefixApp is for app-level state (shared across all users/sessions).
	KeyPrefixApp = "app:"

	// KeyPrefixUser is for user-level state (shared across sessions for a user).
	KeyPrefixUser = "user:"

	// KeyPrefixTemp is for temporary state (discarded after invocation).
	KeyPrefixTemp = "temp:"
)

// ErrStateKeyNotExist is returned when a state key doesn't exist.
var ErrStateKeyNotExist = errors.New("state key does not exist")

// ErrSessionNotFound is returned when a session doesn't exist.
var ErrSessionNotFound = errors.New("session not found")

// memorySession is an in-memory Session implementation.
type memorySession struct {
	id             string
	appName        string
	userID         string
	state          *memoryState
	events         *memoryEvents
	lastUpdateTime time.Time
	mu             sync.RWMutex
}

func (s *memorySession) ID() string      { return s.id }
func (s *memorySession) AppName() string { return s.appName }
func (s *memorySession) UserID() string  { return s.userID }
func (s *memorySession) State() agent.State {
	return s.state
}
func (s *memorySession) Events() agent.Events {
	return s.events
}
func (s *memorySession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastUpdateTime
}

func (s *memorySession) appendEvent(event *agent.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events.append(event)
	s.lastUpdateTime = time.Now()
}

// memoryState is an in-memory State implementation.
type memoryState struct {
	data map[string]any
	mu   sync.RWMutex
}

func newMemoryState(initial map[string]any) *memoryState {
	data := make(map[string]any)
	for k, v := range initial {
		data[k] = v
	}
	return &memoryState{data: data}
}

func (s *memoryState) Get(key string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	if !ok {
		return nil, ErrStateKeyNotExist
	}
	return val, nil
}

func (s *memoryState) Set(key string, val any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return nil
}

func (s *memoryState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for k, v := range s.data {
			if !yield(k, v) {
				return
			}
		}
	}
}

func (s *memoryState) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

// ClearTempKeys removes all keys with the temp: prefix.
// This should be called after each invocation completes.
func (s *memoryState) ClearTempKeys() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.data {
		if strings.HasPrefix(key, KeyPrefixTemp) {
			delete(s.data, key)
		}
	}
}

// memoryEvents is an in-memory Events implementation.
type memoryEvents struct {
	events []*agent.Event
	mu     sync.RWMutex
}

func (e *memoryEvents) All() iter.Seq[*agent.Event] {
	return func(yield func(*agent.Event) bool) {
		e.mu.RLock()
		defer e.mu.RUnlock()
		for _, ev := range e.events {
			if !yield(ev) {
				return
			}
		}
	}
}

func (e *memoryEvents) Len() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.events)
}

func (e *memoryEvents) At(i int) *agent.Event {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if i < 0 || i >= len(e.events) {
		return nil
	}
	return e.events[i]
}

func (e *memoryEvents) append(event *agent.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, event)
}

// InMemoryService returns an in-memory session service.
// Useful for testing and development.
func InMemoryService() Service {
	return &inMemoryService{
		sessions: make(map[string]*memorySession),
	}
}

type inMemoryService struct {
	sessions map[string]*memorySession
	mu       sync.RWMutex
}

func (s *inMemoryService) sessionKey(appName, userID, sessionID string) string {
	return appName + ":" + userID + ":" + sessionID
}

func (s *inMemoryService) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.sessionKey(req.AppName, req.UserID, req.SessionID)
	session, ok := s.sessions[key]
	if !ok {
		return nil, ErrSessionNotFound
	}

	return &GetResponse{Session: session}, nil
}

func (s *inMemoryService) Create(ctx context.Context, req *CreateRequest) (*CreateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	session := &memorySession{
		id:             sessionID,
		appName:        req.AppName,
		userID:         req.UserID,
		state:          newMemoryState(req.State),
		events:         &memoryEvents{},
		lastUpdateTime: time.Now(),
	}

	key := s.sessionKey(req.AppName, req.UserID, sessionID)
	s.sessions[key] = session

	return &CreateResponse{Session: session}, nil
}

func (s *inMemoryService) AppendEvent(ctx context.Context, session Session, event *agent.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.sessionKey(session.AppName(), session.UserID(), session.ID())
	ms, ok := s.sessions[key]
	if !ok {
		return ErrSessionNotFound
	}

	ms.appendEvent(event)
	return nil
}

func (s *inMemoryService) List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sessions []Session
	prefix := req.AppName + ":" + req.UserID + ":"

	for key, session := range s.sessions {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			sessions = append(sessions, session)
		}
	}

	return &ListResponse{Sessions: sessions}, nil
}

func (s *inMemoryService) Delete(ctx context.Context, req *DeleteRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.sessionKey(req.AppName, req.UserID, req.SessionID)
	delete(s.sessions, key)
	return nil
}

var (
	_ Session      = (*memorySession)(nil)
	_ agent.State  = (*memoryState)(nil)
	_ agent.Events = (*memoryEvents)(nil)
	_ Service      = (*inMemoryService)(nil)
)
