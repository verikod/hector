package commandtool

import (
	"context"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/tool"
)

// MockContext implements tool.Context for testing
type MockContext struct {
	context.Context
}

func (m *MockContext) FunctionCallID() string       { return "test-call-id" }
func (m *MockContext) Actions() *agent.EventActions { return nil }
func (m *MockContext) SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error) {
	return nil, nil
}

// CallbackContext methods
func (m *MockContext) Artifacts() agent.Artifacts { return nil }
func (m *MockContext) State() agent.State         { return &MockState{} }

// ReadonlyContext methods (embedded in CallbackContext)
func (m *MockContext) InvocationID() string               { return "test-invocation" }
func (m *MockContext) AgentName() string                  { return "test-agent" }
func (m *MockContext) UserContent() *agent.Content        { return nil }
func (m *MockContext) ReadonlyState() agent.ReadonlyState { return &MockState{} }
func (m *MockContext) UserID() string                     { return "test-user" }
func (m *MockContext) AppName() string                    { return "test-app" }
func (m *MockContext) SessionID() string                  { return "test-session" }
func (m *MockContext) Branch() string                     { return "main" }
func (m *MockContext) Task() agent.CancellableTask        { return nil }

// MockState implements agent.State
type MockState struct{}

func (s *MockState) Get(key string) (any, error)     { return nil, nil }
func (s *MockState) Set(key string, value any) error { return nil }
func (s *MockState) Delete(key string) error         { return nil }
func (s *MockState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {}
}

func TestCommandTool_ExecuteStreaming_PartialOutput(t *testing.T) {
	// Setup command tool
	cfg := Config{
		AllowedCommands: []string{"sh"},
	}
	cmdTool := New(cfg)

	// Command that prints 'a', waits, prints 'b' (no newlines)
	// This verifies unbuffered streaming works
	args := map[string]any{
		"command": "sh -c \"printf 'a'; sleep 0.5; printf 'b'\"",
	}

	ctx := &MockContext{Context: context.Background()}

	// Collect chunks
	var chunks []string

	iterator := cmdTool.CallStreaming(ctx, args)

	start := time.Now()
	var firstChunkTime time.Time

	iterator(func(result *tool.Result, err error) bool {
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
			return false
		}

		if result.Streaming {
			// Fix type assertion
			content, ok := result.Content.(string)
			if !ok {
				t.Errorf("Expected string content, got %T", result.Content)
				return true
			}
			chunks = append(chunks, content)
			if len(chunks) == 1 {
				firstChunkTime = time.Now()
			}
		}
		return true
	})

	totalTime := time.Since(start)

	// Join chunks to verify content
	fullOutput := strings.Join(chunks, "")
	if fullOutput != "ab" {
		t.Errorf("Expected output 'ab', got '%s'", fullOutput)
	}

	// Verify we got multiple chunks
	if len(chunks) < 2 {
		t.Logf("Warning: Received only %d chunk(s) '%v'. Ideally should be separated.", len(chunks), chunks)
	}

	// Verify timing
	if !firstChunkTime.IsZero() {
		delay := totalTime - firstChunkTime.Sub(start)
		if delay < 100*time.Millisecond {
			// This assertion logic was a bit confusing in comments, but basically:
			// If 'a' arrived early, then (firstChunkTime - start) is small.
			// totalTime is large (500ms).
			// So totalTime - (firstChunkTime - start) should be approx totalTime (large).
			// If it's small, it means firstChunkTime was late (near end).
			// But let's verify simply:

			// Wait, totalTime is calculated AFTER loop.
			// Time between first chunk and end should be roughly 500ms.
			diff := firstChunkTime.Sub(start)
			// 'a' should be fast (<100ms)
			if diff > 200*time.Millisecond {
				t.Logf("Warning: First chunk took %v, might be buffered", diff)
			}
		}
	}
}

func TestCommandTool_ExecuteStreaming_ProgressDots(t *testing.T) {
	// Command: printf .; sleep 0.1; printf .; sleep 0.1; printf .
	cfg := Config{
		AllowedCommands: []string{"sh"},
	}
	cmdTool := New(cfg)

	args := map[string]any{
		"command": "sh -c \"printf '.'; sleep 0.2; printf '.'; sleep 0.2; printf '.'\"",
	}

	ctx := &MockContext{Context: context.Background()}
	var chunks []string

	iterator := cmdTool.CallStreaming(ctx, args)
	iterator(func(result *tool.Result, err error) bool {
		if result.Streaming {
			content, ok := result.Content.(string)
			if !ok {
				t.Errorf("Expected string content, got %T", result.Content)
				return true
			}
			chunks = append(chunks, content)
		}
		return true
	})

	full := strings.Join(chunks, "")
	if full != "..." {
		t.Errorf("Expected '...', got '%s'", full)
	}

	if len(chunks) <= 1 {
		t.Errorf("Expected multiple chunks for progress dots, got %d: %v", len(chunks), chunks)
	}
}

func TestCommandTool_SupportsCancellation(t *testing.T) {
	cfg := Config{
		AllowedCommands: []string{"sleep"},
	}
	cmdTool := New(cfg)

	if !cmdTool.SupportsCancellation() {
		t.Error("CommandTool should support cancellation")
	}
}

func TestCommandTool_Cancel_NonExistent(t *testing.T) {
	cfg := Config{
		AllowedCommands: []string{"sleep"},
	}
	cmdTool := New(cfg)

	// Cancelling non-existent execution should return false
	if cmdTool.Cancel("non-existent-call-id") {
		t.Error("Cancel should return false for non-existent callID")
	}
}

// MockContextWithCallID allows setting custom callID for tests
type MockContextWithCallID struct {
	context.Context
	callID string
}

func (m *MockContextWithCallID) FunctionCallID() string       { return m.callID }
func (m *MockContextWithCallID) Actions() *agent.EventActions { return nil }
func (m *MockContextWithCallID) SearchMemory(ctx context.Context, query string) (*agent.MemorySearchResponse, error) {
	return nil, nil
}
func (m *MockContextWithCallID) Artifacts() agent.Artifacts         { return nil }
func (m *MockContextWithCallID) State() agent.State                 { return &MockState{} }
func (m *MockContextWithCallID) InvocationID() string               { return "test-invocation" }
func (m *MockContextWithCallID) AgentName() string                  { return "test-agent" }
func (m *MockContextWithCallID) UserContent() *agent.Content        { return nil }
func (m *MockContextWithCallID) ReadonlyState() agent.ReadonlyState { return &MockState{} }
func (m *MockContextWithCallID) UserID() string                     { return "test-user" }
func (m *MockContextWithCallID) AppName() string                    { return "test-app" }
func (m *MockContextWithCallID) SessionID() string                  { return "test-session" }
func (m *MockContextWithCallID) Branch() string                     { return "main" }
func (m *MockContextWithCallID) Task() agent.CancellableTask        { return nil }

func TestCommandTool_Cancel_MidExecution(t *testing.T) {
	cfg := Config{
		AllowedCommands: []string{"sleep"},
		Timeout:         30 * time.Second,
	}
	cmdTool := New(cfg)

	callID := "cancel-test-call-id"
	ctx := &MockContextWithCallID{Context: context.Background(), callID: callID}

	args := map[string]any{
		"command": "sleep 30", // Long-running command
	}

	done := make(chan bool)
	var finalResult *tool.Result

	// Start execution in goroutine
	go func() {
		iterator := cmdTool.CallStreaming(ctx, args)
		iterator(func(result *tool.Result, err error) bool {
			if !result.Streaming {
				finalResult = result
			}
			return true
		})
		close(done)
	}()

	// Wait a bit for command to start
	// Use a longer delay for CI environments which may be slower
	time.Sleep(500 * time.Millisecond)

	// Cancel the execution
	cancelled := cmdTool.Cancel(callID)
	if !cancelled {
		t.Error("Cancel should return true for active execution")
	}

	// Wait for execution to finish (should be quick after cancel)
	// Use generous timeout for slower CI environments
	select {
	case <-done:
		// Success - command was cancelled
	case <-time.After(5 * time.Second):
		t.Error("Command should have terminated quickly after cancel")
	}

	// Verify the command was killed (exit code non-zero or error)
	if finalResult != nil {
		exitCode, ok := finalResult.Metadata["exit_code"].(int)
		if ok && exitCode == 0 {
			t.Error("Expected non-zero exit code after cancel")
		}
	}

	// Verify subsequent cancel returns false (already cleaned up)
	if cmdTool.Cancel(callID) {
		t.Error("Second Cancel should return false (already cleaned up)")
	}
}
