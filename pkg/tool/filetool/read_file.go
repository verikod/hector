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

package filetool

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// ReadFileArgs defines the parameters for reading a file.
type ReadFileArgs struct {
	Path        string `json:"path" jsonschema:"required,description=File path to read (relative to working directory)"`
	StartLine   int    `json:"start_line,omitempty" jsonschema:"description=Starting line number (1-indexed),minimum=1"`
	EndLine     int    `json:"end_line,omitempty" jsonschema:"description=Ending line number (inclusive),minimum=1"`
	LineNumbers bool   `json:"line_numbers,omitempty" jsonschema:"description=Include line numbers in output,default=true"`
}

// ReadFileConfig defines configuration for the read_file tool.
type ReadFileConfig struct {
	MaxFileSize      int64
	WorkingDirectory string
}

// NewReadFile creates a new read_file tool using FunctionTool.
func NewReadFile(cfg *ReadFileConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &ReadFileConfig{
			MaxFileSize:      10485760, // 10MB default
			WorkingDirectory: "./",
		}
	}

	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = 10485760
	}
	if cfg.WorkingDirectory == "" {
		cfg.WorkingDirectory = "./"
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "read_file",
			Description: "Read file contents. Use this to examine code structure and logic before making edits. For large files, use start_line/end_line to read specific sections to save context window. Always read a file before editing it to ensure you have the latest content.",
		},
		func(ctx tool.Context, args ReadFileArgs) (map[string]any, error) {
			return readFileImpl(cfg, args)
		},
		func(args ReadFileArgs) error {
			return validatePath(cfg.WorkingDirectory, args.Path)
		},
	)
}

func readFileImpl(cfg *ReadFileConfig, args ReadFileArgs) (map[string]any, error) {
	fullPath := filepath.Join(cfg.WorkingDirectory, args.Path)

	// Check file info
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if fileInfo.Size() > cfg.MaxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", fileInfo.Size(), cfg.MaxFileSize)
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// Determine line range
	startLine := 1
	if args.StartLine > 0 {
		startLine = args.StartLine
		if startLine > totalLines {
			return nil, fmt.Errorf("start_line (%d) exceeds file length (%d lines)", startLine, totalLines)
		}
	}

	endLine := totalLines
	if args.EndLine > 0 {
		endLine = args.EndLine
		if endLine > totalLines {
			endLine = totalLines
		}
	}

	if startLine > endLine {
		return nil, fmt.Errorf("invalid range: start_line (%d) > end_line (%d)", startLine, endLine)
	}

	// Default line_numbers to true per schema default and legacy behavior
	showLineNumbers := true
	// If LineNumbers is explicitly set to false, honor that
	// Note: We can't distinguish unset from false in Go, but schema default is true
	// So we default to true and only use false if explicitly set
	// Only allow false when a range is specified (legacy behavior)
	if !args.LineNumbers && (args.StartLine > 0 || args.EndLine > 0) {
		// Explicitly set to false with a range - honor that
		showLineNumbers = false
	}

	// Build output
	var output strings.Builder
	output.WriteString(fmt.Sprintf("FILE: %s\n", args.Path))
	output.WriteString(fmt.Sprintf("STATS: Total lines: %d", totalLines))

	if startLine != 1 || endLine != totalLines {
		output.WriteString(fmt.Sprintf(" | Showing lines %d-%d", startLine, endLine))
	}
	output.WriteString("\n")
	output.WriteString(strings.Repeat("─", 60) + "\n")

	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		if showLineNumbers {
			output.WriteString(fmt.Sprintf("%6d| %s\n", i+1, lines[i]))
		} else {
			output.WriteString(fmt.Sprintf("%s\n", lines[i]))
		}
	}

	output.WriteString(strings.Repeat("─", 60))

	return map[string]any{
		"content":      output.String(),
		"path":         args.Path,
		"total_lines":  totalLines,
		"start_line":   startLine,
		"end_line":     endLine,
		"lines_shown":  endLine - startLine + 1,
		"file_size":    fileInfo.Size(),
		"line_numbers": showLineNumbers,
	}, nil
}

func validatePath(workingDir, path string) error {
	// No absolute paths
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths not allowed, use relative paths")
	}

	// No directory traversal
	cleaned := filepath.Clean(path)
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("directory traversal not allowed (..)")
	}

	// Ensure path is within working directory
	absPath, err := filepath.Abs(filepath.Join(workingDir, cleaned))
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	absWorkDir, err := filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("invalid working directory: %w", err)
	}

	if !strings.HasPrefix(absPath, absWorkDir) {
		return fmt.Errorf("path escapes working directory")
	}

	// Check file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", path)
	}

	return nil
}
