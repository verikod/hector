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
	"log/slog"

	"github.com/verikod/hector/pkg/session"
)

// Manager orchestrates checkpointing and recovery operations.
//
// It provides a unified interface for:
//   - Creating checkpoints during execution
//   - Recovering from checkpoints on startup
//   - Managing checkpoint lifecycle
type Manager struct {
	config   *Config
	storage  *Storage
	recovery *RecoveryManager
}

// NewManager creates a new checkpoint Manager.
func NewManager(cfg *Config, sessionService session.Service) *Manager {
	if cfg == nil {
		cfg = &Config{}
		cfg.SetDefaults()
	}

	storage := NewStorage(sessionService)
	recovery := NewRecoveryManager(cfg, storage)

	return &Manager{
		config:   cfg,
		storage:  storage,
		recovery: recovery,
	}
}

// IsEnabled returns whether checkpointing is enabled.
func (m *Manager) IsEnabled() bool {
	return m.config.IsEnabled()
}

// SetResumeCallback sets the callback for resuming tasks.
func (m *Manager) SetResumeCallback(cb ResumeCallback) {
	m.recovery.SetResumeCallback(cb)
}

// SaveCheckpoint creates and persists a checkpoint.
func (m *Manager) SaveCheckpoint(ctx context.Context, state *State) error {
	if !m.IsEnabled() {
		return nil
	}
	return m.storage.Save(ctx, state)
}

// LoadCheckpoint retrieves a checkpoint by identifiers.
func (m *Manager) LoadCheckpoint(ctx context.Context, appName, userID, sessionID, taskID string) (*State, error) {
	return m.storage.Load(ctx, appName, userID, sessionID, taskID)
}

// ClearCheckpoint removes a checkpoint.
func (m *Manager) ClearCheckpoint(ctx context.Context, appName, userID, sessionID, taskID string) error {
	return m.storage.Clear(ctx, appName, userID, sessionID, taskID)
}

// RecoverOnStartup recovers pending tasks on startup.
func (m *Manager) RecoverOnStartup(ctx context.Context, appName string) error {
	return m.recovery.RecoverPendingTasks(ctx, appName)
}

// ResumeTask manually resumes a task from checkpoint.
func (m *Manager) ResumeTask(ctx context.Context, appName, userID, sessionID, taskID string, userInput string) error {
	return m.recovery.ResumeTask(ctx, appName, userID, sessionID, taskID, userInput)
}

// GetPendingCheckpoints returns all pending checkpoints for a user.
func (m *Manager) GetPendingCheckpoints(ctx context.Context, appName, userID string) ([]*State, error) {
	return m.recovery.GetPendingCheckpoints(ctx, appName, userID)
}

// GetStats returns statistics about pending checkpoints.
func (m *Manager) GetStats(ctx context.Context, appName string) (*CheckpointStats, error) {
	return m.recovery.GetStats(ctx, appName)
}

// Config returns the checkpoint configuration.
func (m *Manager) Config() *Config {
	return m.config
}

// ShouldCheckpointAtIteration returns whether to checkpoint at the given iteration.
func (m *Manager) ShouldCheckpointAtIteration(iteration int) bool {
	return m.config.ShouldCheckpointAtIteration(iteration)
}

// ShouldCheckpointAfterTools returns whether to checkpoint after tool execution.
func (m *Manager) ShouldCheckpointAfterTools() bool {
	return m.config.ShouldCheckpointAfterTools()
}

// ShouldCheckpointBeforeLLM returns whether to checkpoint before LLM calls.
func (m *Manager) ShouldCheckpointBeforeLLM() bool {
	return m.config.ShouldCheckpointBeforeLLM()
}

// CheckpointHooks provides integration hooks for the runner.
type CheckpointHooks struct {
	manager *Manager
}

// NewCheckpointHooks creates hooks for runner integration.
func NewCheckpointHooks(manager *Manager) *CheckpointHooks {
	if manager == nil {
		return nil
	}
	return &CheckpointHooks{manager: manager}
}

// BeforeLLMCall creates a checkpoint before an LLM API call.
func (h *CheckpointHooks) BeforeLLMCall(ctx context.Context, state *State) {
	if h == nil || !h.manager.ShouldCheckpointBeforeLLM() {
		return
	}

	state.WithPhase(PhasePreLLM)
	if err := h.manager.SaveCheckpoint(ctx, state); err != nil {
		slog.Warn("Failed to save pre-LLM checkpoint",
			"task_id", state.TaskID,
			"error", err)
	}
}

// AfterLLMCall creates a checkpoint after an LLM API call.
func (h *CheckpointHooks) AfterLLMCall(ctx context.Context, state *State) {
	if h == nil || !h.manager.IsEnabled() {
		return
	}

	// PostLLM checkpoints are always created (event-driven) if enabled
	state.WithPhase(PhasePostLLM)
	if err := h.manager.SaveCheckpoint(ctx, state); err != nil {
		slog.Warn("Failed to save post-LLM checkpoint",
			"task_id", state.TaskID,
			"error", err)
	}
}

// BeforeToolExecution creates a checkpoint before tool execution.
func (h *CheckpointHooks) BeforeToolExecution(ctx context.Context, state *State, toolName string) {
	if h == nil || !h.manager.IsEnabled() {
		return
	}

	state.WithPhase(PhaseToolExecution)
	if err := h.manager.SaveCheckpoint(ctx, state); err != nil {
		slog.Warn("Failed to save pre-tool checkpoint",
			"task_id", state.TaskID,
			"tool", toolName,
			"error", err)
	}
}

// AfterToolExecution creates a checkpoint after tool execution.
func (h *CheckpointHooks) AfterToolExecution(ctx context.Context, state *State, toolName string) {
	if h == nil || !h.manager.ShouldCheckpointAfterTools() {
		return
	}

	state.WithPhase(PhasePostTool)
	if err := h.manager.SaveCheckpoint(ctx, state); err != nil {
		slog.Warn("Failed to save post-tool checkpoint",
			"task_id", state.TaskID,
			"tool", toolName,
			"error", err)
	}
}

// OnToolApprovalRequired creates a checkpoint when HITL approval is needed.
func (h *CheckpointHooks) OnToolApprovalRequired(ctx context.Context, state *State, pendingTool *PendingToolCall) {
	if h == nil || !h.manager.IsEnabled() {
		return
	}

	state.WithPhase(PhaseToolApproval).WithPendingToolCall(pendingTool)
	if err := h.manager.SaveCheckpoint(ctx, state); err != nil {
		slog.Warn("Failed to save tool approval checkpoint",
			"task_id", state.TaskID,
			"tool", pendingTool.Name,
			"error", err)
	}
}

// OnIterationEnd creates a checkpoint at end of an iteration.
func (h *CheckpointHooks) OnIterationEnd(ctx context.Context, state *State, iteration int) {
	if h == nil || !h.manager.ShouldCheckpointAtIteration(iteration) {
		return
	}

	state.WithPhase(PhaseIterationEnd).WithType(TypeInterval)
	if err := h.manager.SaveCheckpoint(ctx, state); err != nil {
		slog.Warn("Failed to save iteration checkpoint",
			"task_id", state.TaskID,
			"iteration", iteration,
			"error", err)
	}
}

// OnError creates a checkpoint when an error occurs.
func (h *CheckpointHooks) OnError(ctx context.Context, state *State, err error) {
	if h == nil || !h.manager.IsEnabled() {
		return
	}

	state.WithError(err)
	if saveErr := h.manager.SaveCheckpoint(ctx, state); saveErr != nil {
		slog.Warn("Failed to save error checkpoint",
			"task_id", state.TaskID,
			"original_error", err,
			"save_error", saveErr)
	}
}

// OnComplete clears the checkpoint when execution completes successfully.
func (h *CheckpointHooks) OnComplete(ctx context.Context, appName, userID, sessionID, taskID string) {
	if h == nil || !h.manager.IsEnabled() {
		return
	}

	if err := h.manager.ClearCheckpoint(ctx, appName, userID, sessionID, taskID); err != nil {
		slog.Warn("Failed to clear checkpoint on completion",
			"task_id", taskID,
			"error", err)
	}
}
