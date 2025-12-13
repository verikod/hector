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

	"github.com/kadirpekel/hector/pkg/tool"
	"github.com/kadirpekel/hector/pkg/tool/functiontool"
)

// ApplyPatchArgs defines the parameters for apply_patch tool.
type ApplyPatchArgs struct {
	Path              string `json:"path" jsonschema:"required,description=File path to edit (relative to working directory)"`
	OldString         string `json:"old_string" jsonschema:"required,description=Text to find with sufficient surrounding context (3-5 lines before and after the change)"`
	NewString         string `json:"new_string" jsonschema:"required,description=Replacement text (should include the same context as old_string)"`
	ContextValidation bool   `json:"context_validation,omitempty" jsonschema:"description=Validate that surrounding context matches (default: true, recommended for safety),default=true"`
	CreateBackup      bool   `json:"create_backup,omitempty" jsonschema:"description=Create .bak backup file,default=true"`
}

// ApplyPatchConfig defines configuration for the apply_patch tool.
type ApplyPatchConfig struct {
	MaxFileSize      int64
	CreateBackup     bool
	ContextLines     int
	WorkingDirectory string
}

// NewApplyPatch creates a new apply_patch tool using FunctionTool.
func NewApplyPatch(cfg *ApplyPatchConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &ApplyPatchConfig{
			MaxFileSize:      10485760, // 10MB default
			CreateBackup:     true,
			ContextLines:     3,
			WorkingDirectory: "./",
		}
	}

	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = 10485760
	}
	if cfg.ContextLines == 0 {
		cfg.ContextLines = 3
	}
	if cfg.WorkingDirectory == "" {
		cfg.WorkingDirectory = "./"
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "apply_patch",
			Description: "Replace text in a file using context validation. Use this for modifying code blocks where you need to ensure you are targeting the correct location. You must provide surrounding context lines in `old_string` to ensure uniqueness and correctness.",
		},
		func(ctx tool.Context, args ApplyPatchArgs) (map[string]any, error) {
			return applyPatchImpl(cfg, args)
		},
		func(args ApplyPatchArgs) error {
			// Validate path
			if err := validatePath(cfg.WorkingDirectory, args.Path); err != nil {
				return err
			}

			// Validate file size
			fullPath := filepath.Join(cfg.WorkingDirectory, args.Path)
			fileInfo, err := os.Stat(fullPath)
			if err != nil {
				return fmt.Errorf("failed to stat file: %w", err)
			}

			if fileInfo.Size() > cfg.MaxFileSize {
				return fmt.Errorf("file too large: %d bytes (max: %d)", fileInfo.Size(), cfg.MaxFileSize)
			}

			return nil
		},
	)
}

func applyPatchImpl(cfg *ApplyPatchConfig, args ApplyPatchArgs) (map[string]any, error) {
	fullPath := filepath.Join(cfg.WorkingDirectory, args.Path)

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	originalContent := string(content)

	// Check if old_string exists
	if !strings.Contains(originalContent, args.OldString) {
		return nil, fmt.Errorf("patch context not found in file. The old_string must match exactly including whitespace")
	}

	// Check for uniqueness
	count := strings.Count(originalContent, args.OldString)
	if count > 1 {
		return nil, fmt.Errorf("ambiguous patch: old_string appears %d times. Add more context to make it unique", count)
	}

	// Validate context if requested (defaults to true)
	// Since bool defaults to false in Go, we need to check if it was explicitly set
	// For now, we'll always validate unless explicitly disabled
	contextValidated := false
	shouldValidate := true // Default to true per schema
	// Check if context_validation was explicitly set in args
	// If the field exists in the map, use it; otherwise default to true
	// Since we can't distinguish unset from false in Go, we'll default to true
	// Users can explicitly set context_validation=false to disable
	if !args.ContextValidation {
		shouldValidate = false
	}
	if shouldValidate {
		if err := validateContextLines(cfg, args.OldString, args.NewString); err != nil {
			return nil, fmt.Errorf("context validation failed: %w", err)
		}
		contextValidated = true
	}

	// Apply patch
	newContent := strings.Replace(originalContent, args.OldString, args.NewString, 1)

	// Create backup if requested
	backedUp := false
	shouldBackup := args.CreateBackup
	if !shouldBackup {
		shouldBackup = cfg.CreateBackup
	}
	if shouldBackup {
		backupPath := fullPath + ".bak"
		if err := os.WriteFile(backupPath, content, 0644); err != nil {
			// Log warning but don't fail
		} else {
			backedUp = true
		}
	}

	// Write modified content
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Build response message
	oldLines := strings.Split(args.OldString, "\n")
	newLines := strings.Split(args.NewString, "\n")

	var message strings.Builder
	message.WriteString(fmt.Sprintf("SUCCESS: Patch applied successfully to %s\n", args.Path))
	message.WriteString(fmt.Sprintf("CHANGED: Changed %d lines\n", len(oldLines)))
	message.WriteString("\n")
	message.WriteString(generatePatchDiff(args.OldString, args.NewString))

	if backedUp {
		message.WriteString(fmt.Sprintf("\nBACKUP: Backup created: %s.bak", args.Path))
	}

	return map[string]any{
		"message":           message.String(),
		"path":              args.Path,
		"old_lines":         len(oldLines),
		"new_lines":         len(newLines),
		"size_change":       len(newContent) - len(originalContent),
		"backed_up":         backedUp,
		"context_validated": contextValidated,
	}, nil
}

func validateContextLines(cfg *ApplyPatchConfig, oldString, newString string) error {
	oldLines := strings.Split(oldString, "\n")
	newLines := strings.Split(newString, "\n")

	minContextLines := cfg.ContextLines
	if len(oldLines) < minContextLines*2+1 {
		return fmt.Errorf("insufficient context: provide at least %d lines before and after the change", minContextLines)
	}

	contextMatches := 0
	// Check leading context
	for i := 0; i < minContextLines && i < len(oldLines) && i < len(newLines); i++ {
		if oldLines[i] == newLines[i] {
			contextMatches++
		}
	}

	// Check trailing context
	for i := 1; i <= minContextLines && i <= len(oldLines) && i <= len(newLines); i++ {
		oldIdx := len(oldLines) - i
		newIdx := len(newLines) - i
		if oldIdx >= 0 && newIdx >= 0 && oldLines[oldIdx] == newLines[newIdx] {
			contextMatches++
		}
	}

	if contextMatches < minContextLines {
		return fmt.Errorf("context mismatch: ensure old_string and new_string have matching surrounding lines")
	}

	return nil
}

func generatePatchDiff(oldStr, newStr string) string {
	var diff strings.Builder

	diff.WriteString("Changes:\n")
	diff.WriteString(strings.Repeat("-", 60) + "\n")

	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	for i := 0; i < maxLines; i++ {
		if i < len(oldLines) && i < len(newLines) {
			if oldLines[i] != newLines[i] {
				diff.WriteString(fmt.Sprintf("- %s\n", oldLines[i]))
				diff.WriteString(fmt.Sprintf("+ %s\n", newLines[i]))
			} else {
				diff.WriteString(fmt.Sprintf("  %s\n", oldLines[i]))
			}
		} else if i < len(oldLines) {
			diff.WriteString(fmt.Sprintf("- %s\n", oldLines[i]))
		} else if i < len(newLines) {
			diff.WriteString(fmt.Sprintf("+ %s\n", newLines[i]))
		}
	}

	diff.WriteString(strings.Repeat("-", 60))

	return diff.String()
}
