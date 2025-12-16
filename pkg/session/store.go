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

package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"

	"github.com/verikod/hector/pkg/agent"

	// SQL drivers
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// SQLSessionService implements Service using a SQL database.
// Concurrency is handled by database-level locking (transactions).
type SQLSessionService struct {
	db      *sql.DB
	dialect string
}

// sessionRow maps to the sessions table.
type sessionRow struct {
	AppName   string
	UserID    string
	ID        string
	StateJSON string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// eventRow maps to the session_events table (normalized).
type eventRow struct {
	ID           string
	AppName      string
	UserID       string
	SessionID    string
	Author       string
	InvocationID string
	Branch       string

	// Message content
	Role        string
	ContentJSON string // a2a.Message.Parts as JSON

	// Actions (normalized)
	StateDeltaJSON    string
	ArtifactDeltaJSON string
	TransferToAgent   string
	Escalate          bool
	RequireInput      bool
	InputPrompt       string

	// Control flow
	Partial            bool
	TurnComplete       bool
	Interrupted        bool
	LongRunningToolIDs string // JSON array

	// Error info
	ErrorCode    string
	ErrorMessage string

	// Rich content
	ThinkingJSON    string // ThinkingState as JSON
	ToolCallsJSON   string // []ToolCallState as JSON
	ToolResultsJSON string // []ToolResultState as JSON
	MetadataJSON    string // CustomMetadata as JSON

	SequenceNum int
	CreatedAt   time.Time
}

// Schema creation SQL
const createSessionsSchemaSQL = `
CREATE TABLE IF NOT EXISTS sessions (
    app_name VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    id VARCHAR(255) NOT NULL,
    state_json TEXT,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (app_name, user_id, id)
)`

const createSessionsIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(app_name, user_id)`

const createAppStatesSchemaSQL = `
CREATE TABLE IF NOT EXISTS app_states (
    app_name VARCHAR(255) PRIMARY KEY,
    state_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL
)`

const createUserStatesSchemaSQL = `
CREATE TABLE IF NOT EXISTS user_states (
    app_name VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    state_json TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (app_name, user_id)
)`

const createEventsSchemaSQL = `
CREATE TABLE IF NOT EXISTS session_events (
    id VARCHAR(255) NOT NULL,
    app_name VARCHAR(255) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    session_id VARCHAR(255) NOT NULL,
    author VARCHAR(255),
    invocation_id VARCHAR(255),
    branch VARCHAR(255),
    role VARCHAR(50),
    content_json TEXT,
    state_delta_json TEXT,
    artifact_delta_json TEXT,
    transfer_to_agent VARCHAR(255),
    escalate BOOLEAN DEFAULT FALSE,
    require_input BOOLEAN DEFAULT FALSE,
    input_prompt TEXT,
    partial BOOLEAN DEFAULT FALSE,
    turn_complete BOOLEAN DEFAULT FALSE,
    interrupted BOOLEAN DEFAULT FALSE,
    long_running_tool_ids TEXT,
    error_code VARCHAR(100),
    error_message TEXT,
    thinking_json TEXT,
    tool_calls_json TEXT,
    tool_results_json TEXT,
    metadata_json TEXT,
    sequence_num INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    PRIMARY KEY (app_name, user_id, session_id, id)
)`

const createEventsIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_events_session ON session_events(app_name, user_id, session_id, sequence_num)`

const createEventsCreatedAtIndexSQL = `
CREATE INDEX IF NOT EXISTS idx_events_created_at ON session_events(app_name, user_id, session_id, created_at)`

// NewSQLSessionService creates a new SQL-based session service.
func NewSQLSessionService(db *sql.DB, dialect string) (*SQLSessionService, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	switch dialect {
	case "postgres", "mysql", "sqlite", "sqlite3":
		if dialect == "sqlite3" {
			dialect = "sqlite"
		}
	default:
		return nil, fmt.Errorf("unsupported dialect: %s (supported: postgres, mysql, sqlite)", dialect)
	}

	s := &SQLSessionService{
		db:      db,
		dialect: dialect,
	}

	if err := s.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return s, nil
}

// initSchema creates the required tables if they don't exist.
func (s *SQLSessionService) initSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Execute each statement separately for SQLite compatibility
	statements := []string{
		createSessionsSchemaSQL,
		createSessionsIndexSQL,
		createAppStatesSchemaSQL,
		createUserStatesSchemaSQL,
		createEventsSchemaSQL,
		createEventsIndexSQL,
		createEventsCreatedAtIndexSQL,
	}

	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to execute schema statement: %w", err)
		}
	}

	return nil
}

// Close closes the database connection.
func (s *SQLSessionService) Close() error {
	return s.db.Close()
}

// =============================================================================
// Service Implementation
// =============================================================================

// Get retrieves an existing session.
func (s *SQLSessionService) Get(ctx context.Context, req *GetRequest) (*GetResponse, error) {
	// No mutex needed - DB handles concurrent reads
	// Fetch session
	session, err := s.getSession(ctx, req.AppName, req.UserID, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Fetch and merge states
	appState, err := s.getAppState(ctx, req.AppName)
	if err != nil {
		return nil, fmt.Errorf("failed to get app state: %w", err)
	}

	userState, err := s.getUserState(ctx, req.AppName, req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user state: %w", err)
	}

	// Merge states with proper prefixes
	mergedState := mergeStates(appState, userState, session.state.data)
	session.state = newMemoryState(mergedState)

	// Load events with optional filtering
	events, err := s.getEventsFiltered(ctx, req.AppName, req.UserID, req.SessionID, req.NumRecentEvents, req.After)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	session.events = &memoryEvents{events: events}

	return &GetResponse{Session: session}, nil
}

// Create creates a new session.
func (s *SQLSessionService) Create(ctx context.Context, req *CreateRequest) (*CreateResponse, error) {
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	now := time.Now()

	// Extract state deltas by prefix
	appDelta, userDelta, sessionState := extractStateDeltas(req.State)

	// Use transaction for atomic creation of session and state updates
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback if not committed

	// Update app state if needed
	if len(appDelta) > 0 {
		if err := s.upsertAppStateTx(ctx, tx, req.AppName, appDelta); err != nil {
			return nil, fmt.Errorf("failed to save app state: %w", err)
		}
	}

	// Update user state if needed
	if len(userDelta) > 0 {
		if err := s.upsertUserStateTx(ctx, tx, req.AppName, req.UserID, userDelta); err != nil {
			return nil, fmt.Errorf("failed to save user state: %w", err)
		}
	}

	// Create session with session-level state only
	stateJSON, err := json.Marshal(sessionState)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	query := s.insertSessionQuery()
	_, err = tx.ExecContext(ctx, query,
		req.AppName, req.UserID, sessionID, string(stateJSON), now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Fetch merged state for response (outside transaction)
	appState, _ := s.getAppState(ctx, req.AppName)
	userState, _ := s.getUserState(ctx, req.AppName, req.UserID)
	mergedState := mergeStates(appState, userState, sessionState)

	session := &memorySession{
		id:             sessionID,
		appName:        req.AppName,
		userID:         req.UserID,
		state:          newMemoryState(mergedState),
		events:         &memoryEvents{},
		lastUpdateTime: now,
	}

	return &CreateResponse{Session: session}, nil
}

// ErrStaleSession is returned when attempting to modify a session that has been
// updated elsewhere since it was loaded.
var ErrStaleSession = fmt.Errorf("stale session: session has been modified since it was loaded")

// AppendEvent adds an event to the session history.
// Uses optimistic concurrency control to detect stale sessions.
func (s *SQLSessionService) AppendEvent(ctx context.Context, session Session, event *agent.Event) error {
	if session == nil {
		return fmt.Errorf("session is nil")
	}
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	// Skip partial events (streaming chunks)
	if event.Partial {
		return nil
	}

	// Use transaction for atomic updates (no Go mutex needed - DB handles locking)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // Rollback if not committed

	// Stale session check (optimistic concurrency control)
	// This detects if another process/server modified the session since we loaded it.
	// For single-server deployments (SQLite), we use a more lenient check since
	// timestamp precision issues between Go and SQLite can cause false positives.
	if ms, ok := session.(*memorySession); ok {
		dbUpdatedAt, err := s.getSessionUpdatedAtTx(ctx, tx, session.AppName(), session.UserID(), session.ID())
		if err != nil {
			return fmt.Errorf("failed to check session staleness: %w", err)
		}

		// Use second-level precision for comparison to avoid DB timestamp truncation issues.
		// SQLite stores timestamps as TEXT and may lose sub-second precision.
		// For stricter checking in multi-server deployments (PostgreSQL), consider
		// using a version counter instead of timestamps.
		dbSec := dbUpdatedAt.Unix()
		localSec := ms.lastUpdateTime.Unix()
		if dbSec > localSec+1 { // Allow 1 second tolerance
			return fmt.Errorf("%w: db=%s, local=%s", ErrStaleSession,
				dbUpdatedAt.Format(time.RFC3339),
				ms.lastUpdateTime.Format(time.RFC3339))
		}
	}

	// Trim temp state before persisting
	stateDelta := trimTempState(event.Actions.StateDelta)

	// Extract and persist state deltas
	appDelta, userDelta, sessionDelta := extractStateDeltas(stateDelta)

	if len(appDelta) > 0 {
		if err := s.upsertAppStateTx(ctx, tx, session.AppName(), appDelta); err != nil {
			return fmt.Errorf("failed to save app state: %w", err)
		}
	}

	if len(userDelta) > 0 {
		if err := s.upsertUserStateTx(ctx, tx, session.AppName(), session.UserID(), userDelta); err != nil {
			return fmt.Errorf("failed to save user state: %w", err)
		}
	}

	if len(sessionDelta) > 0 {
		if err := s.updateSessionStateTx(ctx, tx, session.AppName(), session.UserID(), session.ID(), sessionDelta); err != nil {
			return fmt.Errorf("failed to update session state: %w", err)
		}
	}

	// Get next sequence number
	seqNum, err := s.getNextSequenceNumTx(ctx, tx, session.AppName(), session.UserID(), session.ID())
	if err != nil {
		return fmt.Errorf("failed to get sequence number: %w", err)
	}

	// Persist the event
	if err := s.insertEventTx(ctx, tx, session, event, seqNum); err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	// Update session timestamp
	now := time.Now()
	if err := s.touchSessionTx(ctx, tx, session.AppName(), session.UserID(), session.ID(), now); err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Update in-memory session if it's our type
	if ms, ok := session.(*memorySession); ok {
		ms.appendEvent(event)
		ms.lastUpdateTime = now
	}

	return nil
}

// List returns sessions matching the filter criteria.
func (s *SQLSessionService) List(ctx context.Context, req *ListRequest) (*ListResponse, error) {
	// No mutex needed - DB handles concurrent reads
	query := `SELECT app_name, user_id, id, state_json, created_at, updated_at 
              FROM sessions WHERE app_name = ?`
	args := []any{req.AppName}

	if req.UserID != "" {
		query += " AND user_id = ?"
		args = append(args, req.UserID)
	}

	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var row sessionRow
		if err := rows.Scan(&row.AppName, &row.UserID, &row.ID, &row.StateJSON, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}

		session, err := s.rowToSession(&row)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}

	return &ListResponse{Sessions: sessions}, nil
}

// Delete removes a session.
func (s *SQLSessionService) Delete(ctx context.Context, req *DeleteRequest) error {
	// No mutex needed - DB handles concurrent writes
	// Delete events first (foreign key)
	eventQuery := `DELETE FROM session_events WHERE app_name = ? AND user_id = ? AND session_id = ?`
	if s.dialect == "postgres" {
		eventQuery = convertToPostgresPlaceholders(eventQuery)
	}
	if _, err := s.db.ExecContext(ctx, eventQuery, req.AppName, req.UserID, req.SessionID); err != nil {
		return fmt.Errorf("failed to delete events: %w", err)
	}

	// Delete session
	query := `DELETE FROM sessions WHERE app_name = ? AND user_id = ? AND id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}
	if _, err := s.db.ExecContext(ctx, query, req.AppName, req.UserID, req.SessionID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

func (s *SQLSessionService) getSession(ctx context.Context, appName, userID, sessionID string) (*memorySession, error) {
	query := `SELECT app_name, user_id, id, state_json, created_at, updated_at 
              FROM sessions WHERE app_name = ? AND user_id = ? AND id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var row sessionRow
	err := s.db.QueryRowContext(ctx, query, appName, userID, sessionID).Scan(
		&row.AppName, &row.UserID, &row.ID, &row.StateJSON, &row.CreatedAt, &row.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return s.rowToSession(&row)
}

func (s *SQLSessionService) rowToSession(row *sessionRow) (*memorySession, error) {
	var state map[string]any
	if row.StateJSON != "" {
		if err := json.Unmarshal([]byte(row.StateJSON), &state); err != nil {
			return nil, fmt.Errorf("failed to unmarshal state: %w", err)
		}
	}
	if state == nil {
		state = make(map[string]any)
	}

	return &memorySession{
		id:             row.ID,
		appName:        row.AppName,
		userID:         row.UserID,
		state:          newMemoryState(state),
		events:         &memoryEvents{},
		lastUpdateTime: row.UpdatedAt,
	}, nil
}

func (s *SQLSessionService) getAppState(ctx context.Context, appName string) (map[string]any, error) {
	query := `SELECT state_json FROM app_states WHERE app_name = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var stateJSON string
	err := s.db.QueryRowContext(ctx, query, appName).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}

	var state map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, err
	}
	return state, nil
}

func (s *SQLSessionService) getUserState(ctx context.Context, appName, userID string) (map[string]any, error) {
	query := `SELECT state_json FROM user_states WHERE app_name = ? AND user_id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var stateJSON string
	err := s.db.QueryRowContext(ctx, query, appName, userID).Scan(&stateJSON)
	if err == sql.ErrNoRows {
		return make(map[string]any), nil
	}
	if err != nil {
		return nil, err
	}

	var state map[string]any
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return nil, err
	}
	return state, nil
}

//nolint:unused // Reserved for future use
func (s *SQLSessionService) upsertAppState(ctx context.Context, appName string, delta map[string]any) error {
	// Get existing state
	existing, _ := s.getAppState(ctx, appName)
	maps.Copy(existing, delta)

	stateJSON, err := json.Marshal(existing)
	if err != nil {
		return err
	}

	query := s.upsertAppStateQuery()
	_, err = s.db.ExecContext(ctx, query, appName, string(stateJSON), time.Now())
	return err
}

// =============================================================================
// Transaction-based helpers (for atomic AppendEvent)
// =============================================================================

func (s *SQLSessionService) getSessionUpdatedAtTx(ctx context.Context, tx *sql.Tx, appName, userID, sessionID string) (time.Time, error) {
	query := `SELECT updated_at FROM sessions WHERE app_name = ? AND user_id = ? AND id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var updatedAt time.Time
	err := tx.QueryRowContext(ctx, query, appName, userID, sessionID).Scan(&updatedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, ErrSessionNotFound
	}
	return updatedAt, err
}

func (s *SQLSessionService) upsertAppStateTx(ctx context.Context, tx *sql.Tx, appName string, delta map[string]any) error {
	// Get existing state within transaction
	query := `SELECT state_json FROM app_states WHERE app_name = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var stateJSON string
	err := tx.QueryRowContext(ctx, query, appName).Scan(&stateJSON)
	existing := make(map[string]any)
	if err == nil && stateJSON != "" {
		_ = json.Unmarshal([]byte(stateJSON), &existing)
	}

	maps.Copy(existing, delta)
	newStateJSON, err := json.Marshal(existing)
	if err != nil {
		return err
	}

	upsertQuery := s.upsertAppStateQuery()
	_, err = tx.ExecContext(ctx, upsertQuery, appName, string(newStateJSON), time.Now())
	return err
}

func (s *SQLSessionService) upsertUserStateTx(ctx context.Context, tx *sql.Tx, appName, userID string, delta map[string]any) error {
	// Get existing state within transaction
	query := `SELECT state_json FROM user_states WHERE app_name = ? AND user_id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var stateJSON string
	err := tx.QueryRowContext(ctx, query, appName, userID).Scan(&stateJSON)
	existing := make(map[string]any)
	if err == nil && stateJSON != "" {
		_ = json.Unmarshal([]byte(stateJSON), &existing)
	}

	maps.Copy(existing, delta)
	newStateJSON, err := json.Marshal(existing)
	if err != nil {
		return err
	}

	upsertQuery := s.upsertUserStateQuery()
	_, err = tx.ExecContext(ctx, upsertQuery, appName, userID, string(newStateJSON), time.Now())
	return err
}

func (s *SQLSessionService) updateSessionStateTx(ctx context.Context, tx *sql.Tx, appName, userID, sessionID string, delta map[string]any) error {
	query := `SELECT state_json FROM sessions WHERE app_name = ? AND user_id = ? AND id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var stateJSON string
	if err := tx.QueryRowContext(ctx, query, appName, userID, sessionID).Scan(&stateJSON); err != nil {
		return err
	}

	var existing map[string]any
	if stateJSON != "" {
		if err := json.Unmarshal([]byte(stateJSON), &existing); err != nil {
			return err
		}
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	maps.Copy(existing, delta)

	newStateJSON, err := json.Marshal(existing)
	if err != nil {
		return err
	}

	updateQuery := `UPDATE sessions SET state_json = ? WHERE app_name = ? AND user_id = ? AND id = ?`
	if s.dialect == "postgres" {
		updateQuery = convertToPostgresPlaceholders(updateQuery)
	}
	_, err = tx.ExecContext(ctx, updateQuery, string(newStateJSON), appName, userID, sessionID)
	return err
}

func (s *SQLSessionService) getNextSequenceNumTx(ctx context.Context, tx *sql.Tx, appName, userID, sessionID string) (int, error) {
	query := `SELECT COALESCE(MAX(sequence_num), 0) + 1 FROM session_events 
              WHERE app_name = ? AND user_id = ? AND session_id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	var seqNum int
	if err := tx.QueryRowContext(ctx, query, appName, userID, sessionID).Scan(&seqNum); err != nil {
		return 0, err
	}
	return seqNum, nil
}

func (s *SQLSessionService) insertEventTx(ctx context.Context, tx *sql.Tx, session Session, event *agent.Event, seqNum int) error {
	row, err := eventToRow(session, event, seqNum)
	if err != nil {
		return err
	}

	query := s.insertEventQuery()
	_, err = tx.ExecContext(ctx, query,
		row.ID, row.AppName, row.UserID, row.SessionID,
		row.Author, row.InvocationID, row.Branch,
		row.Role, row.ContentJSON,
		row.StateDeltaJSON, row.ArtifactDeltaJSON,
		row.TransferToAgent, row.Escalate, row.RequireInput, row.InputPrompt,
		row.Partial, row.TurnComplete, row.Interrupted, row.LongRunningToolIDs,
		row.ErrorCode, row.ErrorMessage,
		row.ThinkingJSON, row.ToolCallsJSON, row.ToolResultsJSON, row.MetadataJSON,
		row.SequenceNum, row.CreatedAt)
	return err
}

func (s *SQLSessionService) touchSessionTx(ctx context.Context, tx *sql.Tx, appName, userID, sessionID string, now time.Time) error {
	query := `UPDATE sessions SET updated_at = ? WHERE app_name = ? AND user_id = ? AND id = ?`
	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}
	_, err := tx.ExecContext(ctx, query, now, appName, userID, sessionID)
	return err
}

//nolint:unused // Reserved for future use
func (s *SQLSessionService) getEvents(ctx context.Context, appName, userID, sessionID string) ([]*agent.Event, error) {
	return s.getEventsFiltered(ctx, appName, userID, sessionID, 0, time.Time{})
}

func (s *SQLSessionService) getEventsFiltered(ctx context.Context, appName, userID, sessionID string, numRecent int, after time.Time) ([]*agent.Event, error) {
	// Column list for SELECT
	cols := `id, app_name, user_id, session_id, author, invocation_id, branch,
              role, content_json, state_delta_json, artifact_delta_json,
              transfer_to_agent, escalate, require_input, input_prompt,
              partial, turn_complete, interrupted, long_running_tool_ids,
              error_code, error_message,
              thinking_json, tool_calls_json, tool_results_json, metadata_json,
              sequence_num, created_at`

	var query string
	var args []any

	if numRecent > 0 {
		// Use subquery to get N most recent events in chronological order
		// This avoids loading all events into memory and reversing
		query = `SELECT ` + cols + ` FROM (
			SELECT ` + cols + ` FROM session_events 
			WHERE app_name = ? AND user_id = ? AND session_id = ?`
		args = []any{appName, userID, sessionID}

		if !after.IsZero() {
			query += " AND created_at >= ?"
			args = append(args, after)
		}

		query += ` ORDER BY sequence_num DESC LIMIT ?
		) sub ORDER BY sequence_num ASC`
		args = append(args, numRecent)
	} else {
		// No limit - get all events in order
		query = `SELECT ` + cols + ` FROM session_events 
              WHERE app_name = ? AND user_id = ? AND session_id = ?`
		args = []any{appName, userID, sessionID}

		if !after.IsZero() {
			query += " AND created_at >= ?"
			args = append(args, after)
		}

		query += " ORDER BY sequence_num ASC"
	}

	if s.dialect == "postgres" {
		query = convertToPostgresPlaceholders(query)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*agent.Event
	for rows.Next() {
		var row eventRow
		if err := rows.Scan(
			&row.ID, &row.AppName, &row.UserID, &row.SessionID,
			&row.Author, &row.InvocationID, &row.Branch,
			&row.Role, &row.ContentJSON,
			&row.StateDeltaJSON, &row.ArtifactDeltaJSON,
			&row.TransferToAgent, &row.Escalate, &row.RequireInput, &row.InputPrompt,
			&row.Partial, &row.TurnComplete, &row.Interrupted, &row.LongRunningToolIDs,
			&row.ErrorCode, &row.ErrorMessage,
			&row.ThinkingJSON, &row.ToolCallsJSON, &row.ToolResultsJSON, &row.MetadataJSON,
			&row.SequenceNum, &row.CreatedAt,
		); err != nil {
			return nil, err
		}

		event, err := rowToEvent(&row)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

// =============================================================================
// SQL Query Builders (dialect-specific)
// =============================================================================

func (s *SQLSessionService) insertSessionQuery() string {
	switch s.dialect {
	case "postgres":
		return `INSERT INTO sessions (app_name, user_id, id, state_json, created_at, updated_at)
                VALUES ($1, $2, $3, $4, $5, $6)`
	default:
		return `INSERT INTO sessions (app_name, user_id, id, state_json, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?)`
	}
}

func (s *SQLSessionService) upsertAppStateQuery() string {
	switch s.dialect {
	case "postgres":
		return `INSERT INTO app_states (app_name, state_json, updated_at)
                VALUES ($1, $2, $3)
                ON CONFLICT (app_name) DO UPDATE SET state_json = $2, updated_at = $3`
	case "mysql":
		return `INSERT INTO app_states (app_name, state_json, updated_at)
                VALUES (?, ?, ?)
                ON DUPLICATE KEY UPDATE state_json = VALUES(state_json), updated_at = VALUES(updated_at)`
	default: // sqlite
		return `INSERT INTO app_states (app_name, state_json, updated_at)
                VALUES (?, ?, ?)
                ON CONFLICT (app_name) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`
	}
}

func (s *SQLSessionService) upsertUserStateQuery() string {
	switch s.dialect {
	case "postgres":
		return `INSERT INTO user_states (app_name, user_id, state_json, updated_at)
                VALUES ($1, $2, $3, $4)
                ON CONFLICT (app_name, user_id) DO UPDATE SET state_json = $3, updated_at = $4`
	case "mysql":
		return `INSERT INTO user_states (app_name, user_id, state_json, updated_at)
                VALUES (?, ?, ?, ?)
                ON DUPLICATE KEY UPDATE state_json = VALUES(state_json), updated_at = VALUES(updated_at)`
	default: // sqlite
		return `INSERT INTO user_states (app_name, user_id, state_json, updated_at)
                VALUES (?, ?, ?, ?)
                ON CONFLICT (app_name, user_id) DO UPDATE SET state_json = excluded.state_json, updated_at = excluded.updated_at`
	}
}

func (s *SQLSessionService) insertEventQuery() string {
	if s.dialect == "postgres" {
		return `INSERT INTO session_events (
                id, app_name, user_id, session_id,
                author, invocation_id, branch,
                role, content_json,
                state_delta_json, artifact_delta_json,
                transfer_to_agent, escalate, require_input, input_prompt,
                partial, turn_complete, interrupted, long_running_tool_ids,
                error_code, error_message,
                thinking_json, tool_calls_json, tool_results_json, metadata_json,
                sequence_num, created_at)
                VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)`
	}
	return `INSERT INTO session_events (
            id, app_name, user_id, session_id,
            author, invocation_id, branch,
            role, content_json,
            state_delta_json, artifact_delta_json,
            transfer_to_agent, escalate, require_input, input_prompt,
            partial, turn_complete, interrupted, long_running_tool_ids,
            error_code, error_message,
            thinking_json, tool_calls_json, tool_results_json, metadata_json,
            sequence_num, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

// =============================================================================
// Conversion Helpers
// =============================================================================

func eventToRow(session Session, event *agent.Event, seqNum int) (*eventRow, error) {
	row := &eventRow{
		ID:           event.ID,
		AppName:      session.AppName(),
		UserID:       session.UserID(),
		SessionID:    session.ID(),
		Author:       event.Author,
		InvocationID: event.InvocationID,
		Branch:       event.Branch,
		SequenceNum:  seqNum,
		CreatedAt:    event.Timestamp,
	}

	// Message content
	if event.Message != nil {
		row.Role = string(event.Message.Role)
		if len(event.Message.Parts) > 0 {
			partsJSON, err := json.Marshal(event.Message.Parts)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal message parts: %w", err)
			}
			row.ContentJSON = string(partsJSON)
		}
	}

	// Actions
	if len(event.Actions.StateDelta) > 0 {
		b, _ := json.Marshal(event.Actions.StateDelta)
		row.StateDeltaJSON = string(b)
	}
	if len(event.Actions.ArtifactDelta) > 0 {
		b, _ := json.Marshal(event.Actions.ArtifactDelta)
		row.ArtifactDeltaJSON = string(b)
	}
	row.TransferToAgent = event.Actions.TransferToAgent
	row.Escalate = event.Actions.Escalate
	row.RequireInput = event.Actions.RequireInput
	row.InputPrompt = event.Actions.InputPrompt

	// Control flow
	row.Partial = event.Partial
	row.TurnComplete = event.TurnComplete
	row.Interrupted = event.Interrupted
	if len(event.LongRunningToolIDs) > 0 {
		b, _ := json.Marshal(event.LongRunningToolIDs)
		row.LongRunningToolIDs = string(b)
	}

	// Error info
	row.ErrorCode = event.ErrorCode
	row.ErrorMessage = event.ErrorMessage

	// Rich content
	if event.Thinking != nil {
		b, _ := json.Marshal(event.Thinking)
		row.ThinkingJSON = string(b)
	}
	if len(event.ToolCalls) > 0 {
		b, _ := json.Marshal(event.ToolCalls)
		row.ToolCallsJSON = string(b)
	}
	if len(event.ToolResults) > 0 {
		b, _ := json.Marshal(event.ToolResults)
		row.ToolResultsJSON = string(b)
	}
	if len(event.CustomMetadata) > 0 {
		b, _ := json.Marshal(event.CustomMetadata)
		row.MetadataJSON = string(b)
	}

	return row, nil
}

func rowToEvent(row *eventRow) (*agent.Event, error) {
	event := &agent.Event{
		ID:           row.ID,
		Timestamp:    row.CreatedAt,
		InvocationID: row.InvocationID,
		Branch:       row.Branch,
		Author:       row.Author,
		Partial:      row.Partial,
		TurnComplete: row.TurnComplete,
		Interrupted:  row.Interrupted,
		ErrorCode:    row.ErrorCode,
		ErrorMessage: row.ErrorMessage,
		Actions: agent.EventActions{
			TransferToAgent: row.TransferToAgent,
			Escalate:        row.Escalate,
			RequireInput:    row.RequireInput,
			InputPrompt:     row.InputPrompt,
		},
	}

	// Message content - parse A2A parts array
	if row.ContentJSON != "" {
		var rawParts []json.RawMessage
		if err := json.Unmarshal([]byte(row.ContentJSON), &rawParts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal content: %w", err)
		}

		// Convert raw JSON to a2a.Part interfaces
		var parts a2a.ContentParts
		for _, raw := range rawParts {
			part, err := parseA2APart(raw)
			if err != nil {
				return nil, fmt.Errorf("failed to parse part: %w", err)
			}
			if part != nil {
				parts = append(parts, part)
			}
		}

		if len(parts) > 0 {
			event.Message = &a2a.Message{
				Role:  a2a.MessageRole(row.Role),
				Parts: parts,
			}
		}
	}

	// Actions
	if row.StateDeltaJSON != "" {
		var delta map[string]any
		if err := json.Unmarshal([]byte(row.StateDeltaJSON), &delta); err != nil {
			return nil, err
		}
		event.Actions.StateDelta = delta
	}
	if row.ArtifactDeltaJSON != "" {
		var delta map[string]int64
		if err := json.Unmarshal([]byte(row.ArtifactDeltaJSON), &delta); err != nil {
			return nil, err
		}
		event.Actions.ArtifactDelta = delta
	}

	// Long running tools
	if row.LongRunningToolIDs != "" {
		var ids []string
		if err := json.Unmarshal([]byte(row.LongRunningToolIDs), &ids); err != nil {
			return nil, err
		}
		event.LongRunningToolIDs = ids
	}

	// Rich content
	if row.ThinkingJSON != "" {
		var thinking agent.ThinkingState
		if err := json.Unmarshal([]byte(row.ThinkingJSON), &thinking); err != nil {
			return nil, err
		}
		event.Thinking = &thinking
	}
	if row.ToolCallsJSON != "" {
		var calls []agent.ToolCallState
		if err := json.Unmarshal([]byte(row.ToolCallsJSON), &calls); err != nil {
			return nil, err
		}
		event.ToolCalls = calls
	}
	if row.ToolResultsJSON != "" {
		var results []agent.ToolResultState
		if err := json.Unmarshal([]byte(row.ToolResultsJSON), &results); err != nil {
			return nil, err
		}
		event.ToolResults = results
	}
	if row.MetadataJSON != "" {
		var meta map[string]any
		if err := json.Unmarshal([]byte(row.MetadataJSON), &meta); err != nil {
			return nil, err
		}
		event.CustomMetadata = meta
	}

	return event, nil
}

// =============================================================================
// State Helpers
// =============================================================================

// extractStateDeltas splits state by prefix into app, user, and session deltas.
func extractStateDeltas(state map[string]any) (appDelta, userDelta, sessionDelta map[string]any) {
	appDelta = make(map[string]any)
	userDelta = make(map[string]any)
	sessionDelta = make(map[string]any)

	for key, value := range state {
		if strings.HasPrefix(key, KeyPrefixApp) {
			appDelta[strings.TrimPrefix(key, KeyPrefixApp)] = value
		} else if strings.HasPrefix(key, KeyPrefixUser) {
			userDelta[strings.TrimPrefix(key, KeyPrefixUser)] = value
		} else if !strings.HasPrefix(key, KeyPrefixTemp) {
			// Session-level state (not temp)
			sessionDelta[key] = value
		}
	}

	return
}

// mergeStates combines app, user, and session states with proper prefixes.
func mergeStates(appState, userState, sessionState map[string]any) map[string]any {
	merged := make(map[string]any, len(appState)+len(userState)+len(sessionState))

	// Add session state first (no prefix)
	maps.Copy(merged, sessionState)

	// Add app state with prefix
	for k, v := range appState {
		merged[KeyPrefixApp+k] = v
	}

	// Add user state with prefix
	for k, v := range userState {
		merged[KeyPrefixUser+k] = v
	}

	return merged
}

// trimTempState removes temporary keys from state delta.
func trimTempState(state map[string]any) map[string]any {
	if state == nil {
		return nil
	}

	result := make(map[string]any, len(state))
	for k, v := range state {
		if !strings.HasPrefix(k, KeyPrefixTemp) {
			result[k] = v
		}
	}
	return result
}

// convertToPostgresPlaceholders converts ? to $1, $2, etc. in a single pass.
func convertToPostgresPlaceholders(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 20) // Pre-allocate for typical expansion
	paramNum := 1
	for _, c := range query {
		if c == '?' {
			b.WriteString(fmt.Sprintf("$%d", paramNum))
			paramNum++
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// parseA2APart parses a JSON raw message into an a2a.Part interface.
// Supports text, file, and data parts based on the "kind" field.
func parseA2APart(raw json.RawMessage) (a2a.Part, error) {
	// First, peek at the "kind" field
	var peek struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return nil, fmt.Errorf("failed to peek part kind: %w", err)
	}

	switch peek.Kind {
	case "text":
		var part a2a.TextPart
		if err := json.Unmarshal(raw, &part); err != nil {
			return nil, err
		}
		return part, nil
	case "file":
		var part a2a.FilePart
		if err := json.Unmarshal(raw, &part); err != nil {
			return nil, err
		}
		return part, nil
	case "data":
		var part a2a.DataPart
		if err := json.Unmarshal(raw, &part); err != nil {
			return nil, err
		}
		return part, nil
	default:
		// Unknown part type - log and skip
		slog.Debug("Unknown part kind in event", "kind", peek.Kind)
		return nil, nil
	}
}

// Compile-time interface check
var _ Service = (*SQLSessionService)(nil)
