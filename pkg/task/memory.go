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
	"sync"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
)

// MemoryTaskStore is a simple in-memory implementation of a2asrv.TaskStore.
// Used as the default when no persistent storage is configured.
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[a2a.TaskID]*a2a.Task
}

// NewMemoryTaskStore creates a new in-memory task store.
func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{
		tasks: make(map[a2a.TaskID]*a2a.Task),
	}
}

// Save stores a task (implements a2asrv.TaskStore).
func (s *MemoryTaskStore) Save(ctx context.Context, task *a2a.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clone to avoid data races
	t := *task
	s.tasks[task.ID] = &t
	return nil
}

// Get retrieves a task by ID (implements a2asrv.TaskStore).
func (s *MemoryTaskStore) Get(ctx context.Context, id a2a.TaskID) (*a2a.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, ok := s.tasks[id]
	if !ok {
		return nil, a2a.ErrTaskNotFound
	}

	// Return a copy
	t := *task
	return &t, nil
}

// Close closes the store (implements a2asrv.TaskStore).
func (s *MemoryTaskStore) Close() error {
	return nil
}

// Compile-time interface compliance check
var _ a2asrv.TaskStore = (*MemoryTaskStore)(nil)
