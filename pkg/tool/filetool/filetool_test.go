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

package filetool_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/filetool"
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

func TestReadFile(t *testing.T) {
	// Create temp directory and file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	content := "line 1\nline 2\nline 3\nline 4\nline 5"
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &filetool.ReadFileConfig{
		MaxFileSize:      10485760,
		WorkingDirectory: tmpDir,
	}

	readTool, err := filetool.NewReadFile(cfg)
	if err != nil {
		t.Fatalf("Failed to create read_file tool: %v", err)
	}

	// Test basic read
	result, err := readTool.Call(&mockContext{}, map[string]any{
		"path": "test.txt",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["path"] != "test.txt" {
		t.Errorf("Expected path 'test.txt', got %v", result["path"])
	}
	if result["total_lines"] != 5 {
		t.Errorf("Expected 5 lines, got %v", result["total_lines"])
	}

	// Test with line range
	result, err = readTool.Call(&mockContext{}, map[string]any{
		"path":       "test.txt",
		"start_line": 2.0,
		"end_line":   3.0,
	})
	if err != nil {
		t.Fatalf("Call with range failed: %v", err)
	}

	if result["lines_shown"] != 2 {
		t.Errorf("Expected 2 lines shown, got %v", result["lines_shown"])
	}

	// Test invalid path (traversal)
	_, err = readTool.Call(&mockContext{}, map[string]any{
		"path": "../etc/passwd",
	})
	if err == nil {
		t.Error("Expected error for directory traversal")
	}
}

func TestWriteFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &filetool.WriteFileConfig{
		MaxFileSize:       1048576,
		BackupOnOverwrite: true,
		WorkingDirectory:  tmpDir,
	}

	writeTool, err := filetool.NewWriteFile(cfg)
	if err != nil {
		t.Fatalf("Failed to create write_file tool: %v", err)
	}

	// Test write new file
	result, err := writeTool.Call(&mockContext{}, map[string]any{
		"path":    "new.txt",
		"content": "hello world",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["action"] != "created" {
		t.Errorf("Expected action 'created', got %v", result["action"])
	}
	if result["size"] != 11 {
		t.Errorf("Expected size 11, got %v", result["size"])
	}

	// Verify file was created
	content, err := os.ReadFile(filepath.Join(tmpDir, "new.txt"))
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(content) != "hello world" {
		t.Errorf("Expected 'hello world', got %s", string(content))
	}

	// Test overwrite with backup
	result, err = writeTool.Call(&mockContext{}, map[string]any{
		"path":    "new.txt",
		"content": "updated content",
		"backup":  true,
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["action"] != "overwritten" {
		t.Errorf("Expected action 'overwritten', got %v", result["action"])
	}
	if result["backed_up"] != true {
		t.Error("Expected backup to be created")
	}

	// Verify backup exists
	_, err = os.Stat(filepath.Join(tmpDir, "new.txt.bak"))
	if err != nil {
		t.Error("Backup file should exist")
	}

	// Test content too large
	largeCfg := &filetool.WriteFileConfig{
		MaxFileSize:      10,
		WorkingDirectory: tmpDir,
	}
	writeTool2, _ := filetool.NewWriteFile(largeCfg)
	_, err = writeTool2.Call(&mockContext{}, map[string]any{
		"path":    "large.txt",
		"content": "this is way too large",
	})
	if err == nil {
		t.Error("Expected error for content too large")
	}
}

func TestGrepSearch(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	files := map[string]string{
		"file1.go":  "package main\nfunc main() {\n\tfmt.Println(\"hello\")\n}",
		"file2.go":  "package main\nfunc helper() {\n\treturn\n}",
		"file3.txt": "not a go file\nbut has func keyword",
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	cfg := &filetool.GrepSearchConfig{
		MaxResults:       1000,
		MaxFileSize:      10485760,
		WorkingDirectory: tmpDir,
		ContextLines:     1,
	}

	grepTool, err := filetool.NewGrepSearch(cfg)
	if err != nil {
		t.Fatalf("Failed to create grep_search tool: %v", err)
	}

	// Test search for "func"
	result, err := grepTool.Call(&mockContext{}, map[string]any{
		"pattern":   "func",
		"recursive": true,
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	totalMatches := result["total_matches"].(int)
	if totalMatches != 3 {
		t.Errorf("Expected 3 matches, got %d", totalMatches)
	}

	// Test with file pattern filter
	result, err = grepTool.Call(&mockContext{}, map[string]any{
		"pattern":      "func",
		"file_pattern": "*.go",
		"recursive":    true,
	})
	if err != nil {
		t.Fatalf("Call with file pattern failed: %v", err)
	}

	totalMatches = result["total_matches"].(int)
	if totalMatches != 2 {
		t.Errorf("Expected 2 matches in .go files, got %d", totalMatches)
	}

	// Test case insensitive
	result, err = grepTool.Call(&mockContext{}, map[string]any{
		"pattern":          "FUNC",
		"case_insensitive": true,
	})
	if err != nil {
		t.Fatalf("Case insensitive search failed: %v", err)
	}

	totalMatches = result["total_matches"].(int)
	if totalMatches != 3 {
		t.Errorf("Expected 3 matches for case insensitive, got %d", totalMatches)
	}

	// Test invalid regex
	_, err = grepTool.Call(&mockContext{}, map[string]any{
		"pattern": "[invalid(",
	})
	if err == nil {
		t.Error("Expected error for invalid regex")
	}
}

func TestSearchReplace(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	originalContent := "hello world\nline 2\nhello world\nline 4"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &filetool.SearchReplaceConfig{
		MaxReplacements:  100,
		ShowDiff:         true,
		CreateBackup:     true,
		WorkingDirectory: tmpDir,
	}

	searchReplaceTool, err := filetool.NewSearchReplace(cfg)
	if err != nil {
		t.Fatalf("Failed to create search_replace tool: %v", err)
	}

	// Test single replacement
	result, err := searchReplaceTool.Call(&mockContext{}, map[string]any{
		"path":       "test.txt",
		"old_string": "line 2",
		"new_string": "line two",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result["replacements"] != 1 {
		t.Errorf("Expected 1 replacement, got %v", result["replacements"])
	}
	if result["replace_all"] != false {
		t.Error("Expected replace_all to be false")
	}

	// Verify file was modified
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "line two") {
		t.Error("File was not modified correctly")
	}

	// Test replace_all
	result, err = searchReplaceTool.Call(&mockContext{}, map[string]any{
		"path":        "test.txt",
		"old_string":  "hello world",
		"new_string":  "hi world",
		"replace_all": true,
	})
	if err != nil {
		t.Fatalf("Call with replace_all failed: %v", err)
	}

	if result["replacements"] != 2 {
		t.Errorf("Expected 2 replacements, got %v", result["replacements"])
	}

	// Test ambiguous replacement (should fail)
	_, err = searchReplaceTool.Call(&mockContext{}, map[string]any{
		"path":       "test.txt",
		"old_string": "hi world",
		"new_string": "hello world",
	})
	if err == nil {
		t.Error("Expected error for ambiguous replacement")
	}

	// Test file not found
	_, err = searchReplaceTool.Call(&mockContext{}, map[string]any{
		"path":       "nonexistent.txt",
		"old_string": "test",
		"new_string": "test2",
	})
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestApplyPatch(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	// Create content with sufficient context (at least 3 lines before and after)
	originalContent := "package main\n\nimport \"fmt\"\n\n// Main function\ntype App struct {\n\tName string\n}\n\nfunc main() {\n\tfmt.Println(\"hello\")\n\tfmt.Println(\"test\")\n}\n\nfunc helper() {\n\treturn\n}"
	if err := os.WriteFile(testFile, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &filetool.ApplyPatchConfig{
		MaxFileSize:      10485760,
		CreateBackup:     true,
		ContextLines:     3,
		WorkingDirectory: tmpDir,
	}

	applyPatchTool, err := filetool.NewApplyPatch(cfg)
	if err != nil {
		t.Fatalf("Failed to create apply_patch tool: %v", err)
	}

	// Test successful patch with sufficient context (3+ lines before and after)
	result, err := applyPatchTool.Call(&mockContext{}, map[string]any{
		"path":               "test.go",
		"old_string":         "// Main function\ntype App struct {\n\tName string\n}\n\nfunc main() {\n\tfmt.Println(\"hello\")\n\tfmt.Println(\"test\")\n}",
		"new_string":         "// Main function\ntype App struct {\n\tName string\n}\n\nfunc main() {\n\tfmt.Println(\"world\")\n\tfmt.Println(\"test\")\n}",
		"context_validation": true, // Explicitly set to test validation
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// old_string includes trailing newline, so count includes that
	if result["old_lines"] != 9 {
		t.Errorf("Expected 9 old lines (including trailing newline), got %v", result["old_lines"])
	}
	if result["context_validated"] != true {
		t.Error("Expected context validation to be true")
	}

	// Verify file was modified
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if !strings.Contains(string(content), "fmt.Println(\"world\")") {
		t.Error("File was not patched correctly")
	}

	// Test ambiguous patch (should fail)
	_, err = applyPatchTool.Call(&mockContext{}, map[string]any{
		"path":       "test.go",
		"old_string": "func",
		"new_string": "function",
	})
	if err == nil {
		t.Error("Expected error for ambiguous patch")
	}

	// Test patch not found
	_, err = applyPatchTool.Call(&mockContext{}, map[string]any{
		"path":       "test.go",
		"old_string": "nonexistent code",
		"new_string": "new code",
	})
	if err == nil {
		t.Error("Expected error for patch not found")
	}

	// Test insufficient context (single line without surrounding context)
	_, err = applyPatchTool.Call(&mockContext{}, map[string]any{
		"path":               "test.go",
		"old_string":         "fmt.Println(\"world\")",
		"new_string":         "fmt.Println(\"updated\")",
		"context_validation": true,
	})
	if err == nil {
		t.Error("Expected error for insufficient context")
	}
}

func TestToolInterfaces(t *testing.T) {
	// Test that all tools implement CallableTool
	tmpDir := t.TempDir()

	readTool, _ := filetool.NewReadFile(&filetool.ReadFileConfig{WorkingDirectory: tmpDir})
	writeTool, _ := filetool.NewWriteFile(&filetool.WriteFileConfig{WorkingDirectory: tmpDir})
	grepTool, _ := filetool.NewGrepSearch(&filetool.GrepSearchConfig{WorkingDirectory: tmpDir})
	searchReplaceTool, _ := filetool.NewSearchReplace(&filetool.SearchReplaceConfig{WorkingDirectory: tmpDir})
	applyPatchTool, _ := filetool.NewApplyPatch(&filetool.ApplyPatchConfig{WorkingDirectory: tmpDir})

	tools := []tool.CallableTool{readTool, writeTool, grepTool, searchReplaceTool, applyPatchTool}

	for _, tl := range tools {
		if tl.Name() == "" {
			t.Error("Tool name is empty")
		}
		if tl.Description() == "" {
			t.Error("Tool description is empty")
		}
		if tl.Schema() == nil {
			t.Error("Tool schema is nil")
		}
		if tl.IsLongRunning() {
			t.Error("File tools should not be long-running")
		}
	}
}
