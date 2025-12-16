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

// Package task provides task management for Hector v2.
//
// A Task is the unit of work in the A2A protocol. This package implements:
//   - Full task state machine (submitted → working → completed/failed)
//   - Human-in-the-loop (HITL) support with input_required state
//   - Execution state persistence for task resumption
//   - Task history and artifact management
package task

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/google/uuid"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/tool"
)

// State represents the current state of a task.
type State string

const (
	// StateSubmitted means the task has been submitted but not started.
	StateSubmitted State = "submitted"

	// StateWorking means the task is being processed.
	StateWorking State = "working"

	// StateCompleted means the task finished successfully.
	StateCompleted State = "completed"

	// StateFailed means the task failed with an error.
	StateFailed State = "failed"

	// StateCancelled means the task was cancelled.
	StateCancelled State = "cancelled"

	// StateInputRequired means the task is waiting for human input (HITL).
	StateInputRequired State = "input_required"

	// StateAuthRequired means the task needs authentication.
	StateAuthRequired State = "auth_required"

	// StateRejected means the task was rejected (e.g., approval denied).
	StateRejected State = "rejected"
)

// IsTerminal returns whether this state is terminal (no more transitions).
func (s State) IsTerminal() bool {
	switch s {
	case StateCompleted, StateFailed, StateCancelled, StateRejected:
		return true
	}
	return false
}

// IsPending returns whether this state is waiting for something.
func (s State) IsPending() bool {
	switch s {
	case StateInputRequired, StateAuthRequired:
		return true
	}
	return false
}

// Task represents a unit of work in the A2A protocol.
// Tasks have a full state machine and support human-in-the-loop interactions.
type Task struct {
	// ID is the unique identifier for this task.
	ID string

	// ContextID links this task to a session/conversation.
	ContextID string

	// Status contains the current state and message.
	Status Status

	// History is the task-specific message history.
	History []*a2a.Message

	// Artifacts produced by this task.
	Artifacts []a2a.Artifact

	// Metadata contains additional task data.
	Metadata map[string]any

	// InputRequirement specifies what input is needed (for HITL).
	InputRequirement *InputRequirement

	// ExecutionState for task resumption.
	ExecutionState *ExecutionState

	// CreatedAt is when the task was created.
	CreatedAt time.Time

	// UpdatedAt is when the task was last updated.
	UpdatedAt time.Time

	// activeExecs tracks running child executions (tools and sub-agents)
	// for cascade cancellation support.
	activeExecs sync.Map // map[callID]*agent.ChildExecution

	mu sync.RWMutex
}

// Status contains the task state and an optional message.
type Status struct {
	State     State
	Message   *a2a.Message
	Timestamp time.Time
	Error     error // For failed state
}

// InputRequirement describes what human input is needed.
type InputRequirement struct {
	// Type of input required.
	Type InputType

	// Prompt to show the user.
	Prompt *a2a.Message

	// Options available to the user.
	Options []InputOption

	// Timeout for the input request.
	Timeout time.Duration

	// ToolCall is the tool awaiting approval (for tool approval type).
	ToolCall *tool.ToolCall

	// RequestedAt is when the input was requested.
	RequestedAt time.Time
}

// InputType identifies the type of human input required.
type InputType string

const (
	// InputTypeToolApproval requires approval for a tool call.
	InputTypeToolApproval InputType = "tool_approval"

	// InputTypeClarification requires clarifying information.
	InputTypeClarification InputType = "clarification"

	// InputTypeAuthentication requires authentication.
	InputTypeAuthentication InputType = "authentication"

	// InputTypeConfirmation requires confirmation to proceed.
	InputTypeConfirmation InputType = "confirmation"
)

// InputOption represents an option presented to the user.
type InputOption struct {
	// ID is the identifier for this option.
	ID string

	// Label is the display text.
	Label string

	// Value is the value if selected.
	Value any

	// IsDefault indicates this is the default option.
	IsDefault bool
}

// ExecutionState contains the state needed to resume a task.
// This is critical for HITL scenarios where the task pauses for input.
type ExecutionState struct {
	// Phase identifies what phase the execution was in.
	Phase string

	// Iteration is the loop iteration (for tool call loops).
	Iteration int

	// Messages is the conversation history at pause time.
	Messages []*a2a.Message

	// PendingToolCall is the tool call awaiting approval.
	PendingToolCall *tool.ToolCall

	// Thinking accumulated so far.
	ThinkingContent   string
	ThinkingSignature string

	// TextAccumulator for partial text.
	TextAccumulator string

	// Timestamp when execution was paused.
	Timestamp time.Time

	// Custom data for complex scenarios.
	Custom map[string]any
}

// New creates a new task.
func New(contextID string) *Task {
	now := time.Now()
	return &Task{
		ID:        uuid.New().String(),
		ContextID: contextID,
		Status: Status{
			State:     StateSubmitted,
			Timestamp: now,
		},
		History:   make([]*a2a.Message, 0),
		Artifacts: make([]a2a.Artifact, 0),
		Metadata:  make(map[string]any),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// SetStatus updates the task status.
func (t *Task) SetStatus(state State, message *a2a.Message, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Status = Status{
		State:     state,
		Message:   message,
		Timestamp: time.Now(),
		Error:     err,
	}
	t.UpdatedAt = time.Now()
}

// GetStatus returns the current status (thread-safe).
func (t *Task) GetStatus() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status
}

// RequestInput pauses the task for human input.
func (t *Task) RequestInput(req *InputRequirement) {
	t.mu.Lock()
	defer t.mu.Unlock()

	req.RequestedAt = time.Now()
	t.InputRequirement = req
	t.Status = Status{
		State:     StateInputRequired,
		Message:   req.Prompt,
		Timestamp: time.Now(),
	}
	t.UpdatedAt = time.Now()
}

// ProvideInput processes human input and clears the requirement.
func (t *Task) ProvideInput(optionID string) *InputOption {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.InputRequirement == nil {
		return nil
	}

	// Find the selected option
	var selected *InputOption
	for i, opt := range t.InputRequirement.Options {
		if opt.ID == optionID {
			selected = &t.InputRequirement.Options[i]
			break
		}
	}

	// Clear requirement and resume
	t.InputRequirement = nil
	t.Status = Status{
		State:     StateWorking,
		Timestamp: time.Now(),
	}
	t.UpdatedAt = time.Now()

	return selected
}

// SaveExecutionState saves the current execution state for later resumption.
func (t *Task) SaveExecutionState(state *ExecutionState) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state.Timestamp = time.Now()
	t.ExecutionState = state
	t.UpdatedAt = time.Now()
}

// LoadExecutionState returns and clears the saved execution state.
func (t *Task) LoadExecutionState() *ExecutionState {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := t.ExecutionState
	t.ExecutionState = nil
	t.UpdatedAt = time.Now()
	return state
}

// AppendHistory adds a message to the task history.
func (t *Task) AppendHistory(msg *a2a.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.History = append(t.History, msg)
	t.UpdatedAt = time.Now()
}

// AddArtifact adds an artifact to the task.
func (t *Task) AddArtifact(artifact a2a.Artifact) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Artifacts = append(t.Artifacts, artifact)
	t.UpdatedAt = time.Now()
}

// SetMetadata sets a metadata value.
func (t *Task) SetMetadata(key string, value any) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.Metadata[key] = value
	t.UpdatedAt = time.Now()
}

// RegisterExecution registers a child execution (tool or sub-agent) for cascade cancellation.
// Implements agent.CancellableTask interface.
func (t *Task) RegisterExecution(exec *agent.ChildExecution) {
	if exec == nil || exec.CallID == "" {
		return
	}
	t.activeExecs.Store(exec.CallID, exec)
}

// UnregisterExecution removes a child execution from tracking.
// Should be called when execution completes (success or failure).
func (t *Task) UnregisterExecution(callID string) {
	t.activeExecs.Delete(callID)
}

// CancelExecution cancels a specific child execution by callID.
// Returns true if cancellation was initiated.
func (t *Task) CancelExecution(callID string) bool {
	value, ok := t.activeExecs.LoadAndDelete(callID)
	if !ok {
		return false
	}
	exec := value.(*agent.ChildExecution)
	if exec.Cancel != nil {
		return exec.Cancel()
	}
	return false
}

// CancelAllChildren cancels all active child executions (cascade cancellation).
// Returns the count of successfully cancelled and failed cancellations.
func (t *Task) CancelAllChildren() (cancelled int, failed int) {
	t.activeExecs.Range(func(key, value any) bool {
		exec := value.(*agent.ChildExecution)
		if exec.Cancel != nil {
			if exec.Cancel() {
				cancelled++
			} else {
				failed++
			}
		}
		t.activeExecs.Delete(key)
		return true
	})
	return cancelled, failed
}

// Service manages tasks.
type Service interface {
	// Create creates a new task.
	Create(ctx context.Context, contextID string) (*Task, error)

	// GetOrCreate retrieves a task by ID or creates one with that ID if not found.
	// This is used to associate executor tasks with a2a's task IDs.
	GetOrCreate(ctx context.Context, taskID, contextID string) (*Task, error)

	// Get retrieves a task by ID.
	Get(ctx context.Context, taskID string) (*Task, error)

	// Update saves task changes.
	Update(ctx context.Context, task *Task) error

	// Cancel cancels a task.
	Cancel(ctx context.Context, taskID string) error

	// List lists tasks for a context.
	List(ctx context.Context, contextID string) ([]*Task, error)
}

// InMemoryService is an in-memory implementation of Service.
type InMemoryService struct {
	tasks map[string]*Task
	mu    sync.RWMutex
}

// NewInMemoryService creates a new in-memory task service.
func NewInMemoryService() *InMemoryService {
	return &InMemoryService{
		tasks: make(map[string]*Task),
	}
}

// Create creates a new task.
func (s *InMemoryService) Create(_ context.Context, contextID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task := New(contextID)
	s.tasks[task.ID] = task
	return task, nil
}

// GetOrCreate retrieves a task by ID or creates one with that ID if not found.
func (s *InMemoryService) GetOrCreate(_ context.Context, taskID, contextID string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.tasks[taskID]; ok {
		return existing, nil
	}

	// Create new task with the provided ID
	task := New(contextID)
	task.ID = taskID // Override the generated ID with the provided a2a taskID
	s.tasks[taskID] = task
	return task, nil
}

// Get retrieves a task by ID.
func (s *InMemoryService) Get(_ context.Context, taskID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return nil, ErrTaskNotFound
	}
	return task, nil
}

// Update saves task changes.
func (s *InMemoryService) Update(_ context.Context, task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[task.ID]; !ok {
		return ErrTaskNotFound
	}
	s.tasks[task.ID] = task
	return nil
}

// Cancel cancels a task.
func (s *InMemoryService) Cancel(_ context.Context, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}

	if task.Status.State.IsTerminal() {
		return ErrTaskTerminal
	}

	// Cascade: cancel all child executions first
	cancelled, failed := task.CancelAllChildren()
	if cancelled > 0 || failed > 0 {
		slog.Info("Task cascade cancel", "task_id", taskID, "cancelled", cancelled, "failed", failed)
	}

	task.SetStatus(StateCancelled, nil, nil)
	return nil
}

// List lists tasks for a context.
func (s *InMemoryService) List(_ context.Context, contextID string) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*Task
	for _, task := range s.tasks {
		if task.ContextID == contextID {
			result = append(result, task)
		}
	}
	return result, nil
}

// Errors
var (
	ErrTaskNotFound = &TaskError{Code: "task_not_found", Message: "task not found"}
	ErrTaskTerminal = &TaskError{Code: "task_terminal", Message: "task is in terminal state"}
)

// TaskError is a task-related error.
type TaskError struct {
	Code    string
	Message string
}

func (e *TaskError) Error() string {
	return e.Message
}
