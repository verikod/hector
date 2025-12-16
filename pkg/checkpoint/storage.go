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

package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/verikod/hector/pkg/session"
)

const (
	// pendingExecutionsKey is the session state key for storing checkpoints.
	pendingExecutionsKey = "pending_executions"
)

// Storage manages checkpoint persistence.
//
// Architecture (derived from legacy Hector):
//
//	Checkpoints are stored in session state (metadata) under the "pending_executions" key.
//	This keeps checkpoints co-located with the session they belong to, making recovery simple.
//
// Storage layout:
//
//	session.state["pending_executions"] = {
//	    "<task_id>": { ... checkpoint state ... }
//	}
type Storage struct {
	sessionService session.Service
}

// NewStorage creates a new checkpoint Storage.
func NewStorage(sessionService session.Service) *Storage {
	return &Storage{
		sessionService: sessionService,
	}
}

// Save persists a checkpoint state.
func (s *Storage) Save(ctx context.Context, state *State) error {
	if state == nil {
		return fmt.Errorf("cannot save nil checkpoint state")
	}
	if state.TaskID == "" {
		return fmt.Errorf("task_id is required for checkpoint")
	}
	if state.SessionID == "" {
		return fmt.Errorf("session_id is required for checkpoint")
	}

	// Get the session
	sess, err := s.getSession(ctx, state.AppName, state.UserID, state.SessionID)
	if err != nil {
		return fmt.Errorf("failed to get session for checkpoint: %w", err)
	}

	// Serialize checkpoint state
	stateJSON, err := state.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize checkpoint state: %w", err)
	}

	// Convert to map for session state storage
	var stateMap map[string]any
	if err := json.Unmarshal(stateJSON, &stateMap); err != nil {
		return fmt.Errorf("failed to unmarshal checkpoint state: %w", err)
	}

	// Get or create pending_executions map
	pendingMap, err := s.getPendingExecutions(sess)
	if err != nil {
		return err
	}

	// Store checkpoint under task ID
	pendingMap[state.TaskID] = stateMap

	// Persist back to session state
	if err := sess.State().Set(pendingExecutionsKey, pendingMap); err != nil {
		return fmt.Errorf("failed to update session state: %w", err)
	}

	slog.Debug("Saved checkpoint",
		"task_id", state.TaskID,
		"session_id", state.SessionID,
		"phase", state.Phase,
		"type", state.CheckpointType)

	return nil
}

// Load retrieves a checkpoint state for a task.
func (s *Storage) Load(ctx context.Context, appName, userID, sessionID, taskID string) (*State, error) {
	// Get the session
	sess, err := s.getSession(ctx, appName, userID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Get pending_executions map
	pendingMap, err := s.getPendingExecutions(sess)
	if err != nil {
		return nil, err
	}

	// Look up task checkpoint
	taskState, exists := pendingMap[taskID]
	if !exists {
		return nil, fmt.Errorf("no checkpoint found for task %s", taskID)
	}

	// Convert back to State
	stateJSON, err := json.Marshal(taskState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task state: %w", err)
	}

	state, err := Deserialize(stateJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize checkpoint: %w", err)
	}

	slog.Debug("Loaded checkpoint",
		"task_id", taskID,
		"session_id", sessionID,
		"phase", state.Phase)

	return state, nil
}

// Clear removes a checkpoint for a task.
func (s *Storage) Clear(ctx context.Context, appName, userID, sessionID, taskID string) error {
	// Get the session
	sess, err := s.getSession(ctx, appName, userID, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Get pending_executions map
	pendingMap, err := s.getPendingExecutions(sess)
	if err != nil {
		return err
	}

	// Remove task checkpoint
	delete(pendingMap, taskID)

	// Update session state
	if len(pendingMap) == 0 {
		// Remove the key entirely if empty
		if err := sess.State().Delete(pendingExecutionsKey); err != nil {
			slog.Debug("Failed to delete empty pending_executions key", "error", err)
		}
	} else {
		if err := sess.State().Set(pendingExecutionsKey, pendingMap); err != nil {
			return fmt.Errorf("failed to update session state: %w", err)
		}
	}

	slog.Debug("Cleared checkpoint",
		"task_id", taskID,
		"session_id", sessionID)

	return nil
}

// ListPending returns all pending checkpoints for a user.
func (s *Storage) ListPending(ctx context.Context, appName, userID string) ([]*State, error) {
	// List all sessions for the user
	resp, err := s.sessionService.List(ctx, &session.ListRequest{
		AppName:  appName,
		UserID:   userID,
		PageSize: 1000, // Reasonable limit
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var states []*State
	for _, sess := range resp.Sessions {
		// Get pending_executions from each session
		pendingMap, err := s.getPendingExecutions(sess)
		if err != nil {
			continue // Skip sessions without checkpoints
		}

		// Convert each task checkpoint to State
		for taskID, taskState := range pendingMap {
			stateJSON, err := json.Marshal(taskState)
			if err != nil {
				slog.Warn("Failed to marshal checkpoint",
					"task_id", taskID,
					"session_id", sess.ID(),
					"error", err)
				continue
			}

			state, err := Deserialize(stateJSON)
			if err != nil {
				slog.Warn("Failed to deserialize checkpoint",
					"task_id", taskID,
					"session_id", sess.ID(),
					"error", err)
				continue
			}

			states = append(states, state)
		}
	}

	return states, nil
}

// ListAllPending returns all pending checkpoints across all users.
// This is used for startup recovery.
func (s *Storage) ListAllPending(ctx context.Context, appName string) ([]*State, error) {
	// For startup recovery, we need to scan all sessions.
	// This is a potentially expensive operation, so it should be called sparingly.
	//
	// Note: In production, you might want to add an index or separate table for
	// quick checkpoint lookup. For now, we rely on session listing.

	// List sessions without user filter (requires session service to support this)
	resp, err := s.sessionService.List(ctx, &session.ListRequest{
		AppName:  appName,
		UserID:   "", // Empty user ID to list all
		PageSize: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var states []*State
	for _, sess := range resp.Sessions {
		pendingMap, err := s.getPendingExecutions(sess)
		if err != nil {
			continue
		}

		for _, taskState := range pendingMap {
			stateJSON, err := json.Marshal(taskState)
			if err != nil {
				continue
			}

			state, err := Deserialize(stateJSON)
			if err != nil {
				continue
			}

			states = append(states, state)
		}
	}

	slog.Info("Found pending checkpoints",
		"app_name", appName,
		"count", len(states))

	return states, nil
}

// getSession retrieves a session by its identifiers.
func (s *Storage) getSession(ctx context.Context, appName, userID, sessionID string) (session.Session, error) {
	resp, err := s.sessionService.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, err
	}
	return resp.Session, nil
}

// getPendingExecutions retrieves the pending_executions map from session state.
func (s *Storage) getPendingExecutions(sess session.Session) (map[string]any, error) {
	state := sess.State()
	if state == nil {
		return make(map[string]any), nil
	}

	val, err := state.Get(pendingExecutionsKey)
	if err != nil {
		// Key doesn't exist - return empty map
		return make(map[string]any), nil
	}

	pendingMap, ok := val.(map[string]any)
	if !ok {
		// Invalid format - log and return empty
		slog.Warn("Invalid pending_executions format in session",
			"session_id", sess.ID(),
			"type", fmt.Sprintf("%T", val))
		return make(map[string]any), nil
	}

	return pendingMap, nil
}
