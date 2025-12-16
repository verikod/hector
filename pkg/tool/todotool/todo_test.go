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

package todotool_test

import (
	"context"
	"testing"
	"time"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/todotool"
)

type mockContext struct{}

func (m *mockContext) FunctionCallID() string       { return "test-call" }
func (m *mockContext) Actions() *agent.EventActions { return nil }
func (m *mockContext) SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error) {
	return nil, nil
}
func (m *mockContext) Artifacts() agent.Artifacts         { return nil }
func (m *mockContext) State() agent.State                 { return nil }
func (m *mockContext) InvocationID() string               { return "test-inv" }
func (m *mockContext) AgentName() string                  { return "test-agent" }
func (m *mockContext) UserContent() *agent.Content        { return nil }
func (m *mockContext) ReadonlyState() agent.ReadonlyState { return nil }
func (m *mockContext) UserID() string                     { return "test-user" }
func (m *mockContext) AppName() string                    { return "test-app" }
func (m *mockContext) SessionID() string                  { return "test-session" }
func (m *mockContext) Branch() string                     { return "" }
func (m *mockContext) Deadline() (time.Time, bool)        { return time.Time{}, false }
func (m *mockContext) Done() <-chan struct{}              { return nil }
func (m *mockContext) Err() error                         { return nil }
func (m *mockContext) Value(key any) any                  { return nil }
func (m *mockContext) Task() agent.CancellableTask        { return nil }

func TestTodoCreate(t *testing.T) {
	manager := todotool.NewTodoManager()
	todoTool, err := manager.Tool()
	if err != nil {
		t.Fatalf("Failed to create todo tool: %v", err)
	}

	// Create initial todos (Overwrite=true to effectively create/replace)
	result, err := todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": true,
		"todos": []any{
			map[string]any{"id": "1", "content": "Task 1", "status": "pending"},
			map[string]any{"id": "2", "content": "Task 2", "status": "in_progress"},
		},
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["count"] != 2 {
		t.Errorf("Expected 2 todos, got %v", result["count"])
	}

	// Verify todos were stored
	todos := manager.GetTodos("test-session")
	if len(todos) != 2 {
		t.Errorf("Expected 2 todos in manager, got %d", len(todos))
	}
}

func TestTodoMerge(t *testing.T) {
	manager := todotool.NewTodoManager()
	todoTool, _ := manager.Tool()

	// Create initial todos (Overwrite=true)
	_, _ = todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": true,
		"todos": []any{
			map[string]any{"id": "1", "content": "Task 1", "status": "pending"},
			map[string]any{"id": "2", "content": "Task 2", "status": "pending"},
		},
	})

	// Merge: update one, add one new (Overwrite=false or omitted)
	result, err := todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": false,
		"todos": []any{
			map[string]any{"id": "1", "content": "Task 1 Updated", "status": "completed"},
			map[string]any{"id": "3", "content": "Task 3", "status": "pending"},
		},
	})
	if err != nil {
		t.Fatalf("Merge call failed: %v", err)
	}

	if result["count"] != 3 {
		t.Errorf("Expected 3 todos after merge, got %v", result["count"])
	}

	todos := manager.GetTodos("test-session")
	if len(todos) != 3 {
		t.Fatalf("Expected 3 todos, got %d", len(todos))
	}

	// Verify task 1 was updated
	var task1 *todotool.TodoItem
	for _, todo := range todos {
		if todo.ID == "1" {
			task1 = &todo
			break
		}
	}
	if task1 == nil {
		t.Fatal("Task 1 not found")
	}
	if task1.Status != "completed" {
		t.Errorf("Expected task 1 status 'completed', got %s", task1.Status)
	}
	if task1.Content != "Task 1 Updated" {
		t.Errorf("Expected updated content, got %s", task1.Content)
	}
}

func TestTodoValidation(t *testing.T) {
	manager := todotool.NewTodoManager()
	todoTool, _ := manager.Tool()

	// Test empty todos array
	_, err := todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": false,
		"todos":     []any{},
	})
	if err == nil {
		t.Error("Expected error for empty todos array")
	}

	// Test missing required fields
	_, err = todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": false,
		"todos": []any{
			map[string]any{"id": "1", "status": "pending"}, // missing content
		},
	})
	if err == nil {
		t.Error("Expected error for missing content field")
	}

	// Test invalid status
	_, err = todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": false,
		"todos": []any{
			map[string]any{"id": "1", "content": "Task", "status": "invalid"},
		},
	})
	if err == nil {
		t.Error("Expected error for invalid status")
	}

	// Test valid statuses
	validStatuses := []string{"pending", "in_progress", "completed", "canceled"}
	// Test valid statuses
	// validStatuses already declared above
	for _, status := range validStatuses {
		_, err := todoTool.Call(&mockContext{}, map[string]any{
			"overwrite": false,
			"todos": []any{
				map[string]any{"id": "1", "content": "Task", "status": status},
			},
		})
		if err != nil {
			t.Errorf("Valid status %s should not error: %v", status, err)
		}
	}
}

func TestTodoFullUpdate(t *testing.T) {
	manager := todotool.NewTodoManager()
	todoTool, _ := manager.Tool()

	// Initial Create
	_, _ = todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": true,
		"todos": []any{
			map[string]any{"id": "1", "content": "Task 1", "status": "pending"},
			map[string]any{"id": "2", "content": "Task 2", "status": "pending"},
		},
	})

	// Update: Must provide all fields for all todos
	_, err := todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": false,
		"todos": []any{
			map[string]any{"id": "1", "content": "Task 1", "status": "completed"},
			map[string]any{"id": "2", "content": "Task 2", "status": "in_progress"},
		},
	})
	if err != nil {
		t.Fatalf("Full update failed: %v", err)
	}

	todos := manager.GetTodos("test-session")
	if len(todos) != 2 {
		t.Fatalf("Expected 2 todos, got %d", len(todos))
	}
	if todos[0].Status != "completed" {
		t.Errorf("Expected task 1 status 'completed', got '%s'", todos[0].Status)
	}
	if todos[1].Status != "in_progress" {
		t.Errorf("Expected task 2 status 'in_progress', got '%s'", todos[1].Status)
	}

	// Partial updates should now fail (missing required fields)
	_, err = todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": false,
		"todos": []any{
			map[string]any{"id": "1", "status": "canceled"}, // Missing content
		},
	})
	if err == nil {
		t.Error("Expected error for partial update (missing content)")
	}
}

func TestTodoSummary(t *testing.T) {
	manager := todotool.NewTodoManager()
	todoTool, _ := manager.Tool()

	// Create todos with different statuses
	_, _ = todoTool.Call(&mockContext{}, map[string]any{
		"overwrite": true,
		"todos": []any{
			map[string]any{"id": "1", "content": "Pending task", "status": "pending"},
			map[string]any{"id": "2", "content": "In progress task", "status": "in_progress"},
			map[string]any{"id": "3", "content": "Completed task", "status": "completed"},
			map[string]any{"id": "4", "content": "Canceled task", "status": "canceled"},
		},
	})

	summary := manager.GetTodosSummary("test-session")
	if summary == "" {
		t.Error("Summary should not be empty")
	}

	// Summary should contain status counts
	if !containsSubstring(summary, "1 pending") {
		t.Error("Summary should mention 1 pending")
	}
	if !containsSubstring(summary, "1 in progress") {
		t.Error("Summary should mention 1 in progress")
	}
	if !containsSubstring(summary, "1 completed") {
		t.Error("Summary should mention 1 completed")
	}
	if !containsSubstring(summary, "1 canceled") {
		t.Error("Summary should mention 1 canceled")
	}
}

func TestTodoToolInterface(t *testing.T) {
	manager := todotool.NewTodoManager()
	todoTool, _ := manager.Tool()

	// Verify tool interface
	var _ tool.CallableTool = todoTool

	if todoTool.Name() != "todo_write" {
		t.Errorf("Expected name 'todo_write', got %s", todoTool.Name())
	}
	if todoTool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if todoTool.Schema() == nil {
		t.Error("Schema should not be nil")
	}
	if todoTool.IsLongRunning() {
		t.Error("Todo tool should not be long-running")
	}
}

func TestFormatTodosForContext(t *testing.T) {
	todos := []todotool.TodoItem{
		{ID: "1", Content: "Task 1", Status: "pending"},
		{ID: "2", Content: "Task 2", Status: "completed"},
	}

	formatted := todotool.FormatTodosForContext(todos)
	if formatted == "" {
		t.Error("Formatted output should not be empty")
	}

	if !containsSubstring(formatted, "Task 1") {
		t.Error("Should contain Task 1")
	}
	if !containsSubstring(formatted, "Task 2") {
		t.Error("Should contain Task 2")
	}

	// Empty todos should return empty string
	empty := todotool.FormatTodosForContext([]todotool.TodoItem{})
	if empty != "" {
		t.Error("Empty todos should return empty string")
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
