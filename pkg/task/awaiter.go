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

package task

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/verikod/hector/pkg/tool"
)

// Awaiter manages waiting for human input on tasks.
// It supports both blocking (streaming) and async (request/response) modes.
type Awaiter struct {
	// waiters maps task IDs to their input channels
	waiters map[string]chan *InputResponse
	mu      sync.RWMutex

	// defaultTimeout is used when no timeout is specified
	defaultTimeout time.Duration
}

// InputResponse contains the human input response.
type InputResponse struct {
	// OptionID is the selected option (if options were provided).
	OptionID string

	// Value is the input value (for free-form input).
	Value any

	// Approved is true if the action was approved (for approval requests).
	Approved bool

	// Message is an optional message from the user.
	Message string
}

// NewAwaiter creates a new task awaiter.
func NewAwaiter(defaultTimeout time.Duration) *Awaiter {
	if defaultTimeout == 0 {
		defaultTimeout = 5 * time.Minute
	}
	return &Awaiter{
		waiters:        make(map[string]chan *InputResponse),
		defaultTimeout: defaultTimeout,
	}
}

// WaitForInput blocks until input is received or timeout.
// This is used in blocking (streaming) mode.
func (a *Awaiter) WaitForInput(ctx context.Context, task *Task) (*InputResponse, error) {
	if task.InputRequirement == nil {
		return nil, errors.New("task has no input requirement")
	}

	// Create wait channel
	ch := make(chan *InputResponse, 1)
	a.mu.Lock()
	a.waiters[task.ID] = ch
	a.mu.Unlock()

	// Clean up on exit
	defer func() {
		a.mu.Lock()
		delete(a.waiters, task.ID)
		a.mu.Unlock()
		close(ch)
	}()

	// Determine timeout
	timeout := task.InputRequirement.Timeout
	if timeout == 0 {
		timeout = a.defaultTimeout
	}

	// Wait for input or timeout
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, ErrInputTimeout
	case resp := <-ch:
		return resp, nil
	}
}

// ProvideInput provides input for a waiting task.
// This is called from the API handler when the user responds.
func (a *Awaiter) ProvideInput(taskID string, resp *InputResponse) error {
	a.mu.RLock()
	ch, ok := a.waiters[taskID]
	a.mu.RUnlock()

	if !ok {
		return ErrNoWaiter
	}

	select {
	case ch <- resp:
		return nil
	default:
		return ErrWaiterFull
	}
}

// IsWaiting returns whether a task is waiting for input.
func (a *Awaiter) IsWaiting(taskID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	_, ok := a.waiters[taskID]
	return ok
}

// Errors
var (
	ErrInputTimeout = errors.New("input timeout")
	ErrNoWaiter     = errors.New("no waiter for task")
	ErrWaiterFull   = errors.New("waiter channel full")
)

// ApprovalRequest creates a standard tool approval input requirement.
func ApprovalRequest(tc *tool.ToolCall, prompt string, timeout time.Duration) *InputRequirement {
	return &InputRequirement{
		Type:     InputTypeToolApproval,
		ToolCall: tc,
		Timeout:  timeout,
		Options: []InputOption{
			{ID: "approve", Label: "Approve", Value: true, IsDefault: false},
			{ID: "deny", Label: "Deny", Value: false, IsDefault: false},
		},
	}
}

// ConfirmationRequest creates a confirmation input requirement.
func ConfirmationRequest(prompt string, timeout time.Duration) *InputRequirement {
	return &InputRequirement{
		Type:    InputTypeConfirmation,
		Timeout: timeout,
		Options: []InputOption{
			{ID: "yes", Label: "Yes", Value: true, IsDefault: false},
			{ID: "no", Label: "No", Value: false, IsDefault: true},
		},
	}
}

// ClarificationRequest creates a clarification input requirement.
func ClarificationRequest(prompt string, timeout time.Duration) *InputRequirement {
	return &InputRequirement{
		Type:    InputTypeClarification,
		Timeout: timeout,
		Options: nil, // Free-form input
	}
}
