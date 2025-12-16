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

// SearchReplaceArgs defines the parameters for search_replace tool.
type SearchReplaceArgs struct {
	Path         string `json:"path" jsonschema:"required,description=File path to edit (relative to working directory)"`
	OldString    string `json:"old_string" jsonschema:"required,description=Exact text to find (must be unique unless replace_all=true)"`
	NewString    string `json:"new_string" jsonschema:"required,description=Replacement text"`
	ReplaceAll   bool   `json:"replace_all,omitempty" jsonschema:"description=Replace all occurrences (default: false, requires unique match),default=false"`
	ShowDiff     bool   `json:"show_diff,omitempty" jsonschema:"description=Show diff of changes,default=true"`
	CreateBackup bool   `json:"create_backup,omitempty" jsonschema:"description=Create .bak backup file,default=true"`
}

// SearchReplaceConfig defines configuration for the search_replace tool.
type SearchReplaceConfig struct {
	MaxReplacements  int
	ShowDiff         bool
	CreateBackup     bool
	WorkingDirectory string
}

// NewSearchReplace creates a new search_replace tool using FunctionTool.
func NewSearchReplace(cfg *SearchReplaceConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &SearchReplaceConfig{
			MaxReplacements:  100,
			ShowDiff:         true,
			CreateBackup:     true,
			WorkingDirectory: "./",
		}
	}

	if cfg.MaxReplacements == 0 {
		cfg.MaxReplacements = 100
	}
	if cfg.WorkingDirectory == "" {
		cfg.WorkingDirectory = "./"
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "search_replace",
			Description: "Replace exact string matches in a file. Use this for simple, precise edits like variable renames or single-line fixes. The `old_string` must match the target text EXACTLY, including all whitespace and indentation. For multi-line code blocks, consider using `apply_patch`.",
		},
		func(ctx tool.Context, args SearchReplaceArgs) (map[string]any, error) {
			return searchReplaceImpl(cfg, args)
		},
		func(args SearchReplaceArgs) error {
			return validatePath(cfg.WorkingDirectory, args.Path)
		},
	)
}

func searchReplaceImpl(cfg *SearchReplaceConfig, args SearchReplaceArgs) (map[string]any, error) {
	fullPath := filepath.Join(cfg.WorkingDirectory, args.Path)

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	originalContent := string(content)

	// Check if old_string exists
	if !strings.Contains(originalContent, args.OldString) {
		return nil, fmt.Errorf("old_string not found in file: '%s'", truncateString(args.OldString, 50))
	}

	// Count occurrences
	count := strings.Count(originalContent, args.OldString)
	if !args.ReplaceAll && count > 1 {
		return nil, fmt.Errorf("old_string appears %d times - must be unique or use replace_all=true", count)
	}

	if count > cfg.MaxReplacements {
		return nil, fmt.Errorf("too many replacements: %d (max: %d)", count, cfg.MaxReplacements)
	}

	// Perform replacement
	var newContent string
	replacementCount := 0
	if args.ReplaceAll {
		newContent = strings.ReplaceAll(originalContent, args.OldString, args.NewString)
		replacementCount = count
	} else {
		newContent = strings.Replace(originalContent, args.OldString, args.NewString, 1)
		replacementCount = 1
	}

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
			// In v2, we don't have direct access to logger, so we'll include it in metadata
		} else {
			backedUp = true
		}
	}

	// Write modified content
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	// Build response message
	var message strings.Builder
	message.WriteString(fmt.Sprintf("SUCCESS: Replaced %d occurrence(s) in %s\n", replacementCount, args.Path))

	// Add diff if requested
	shouldShowDiff := args.ShowDiff
	if !shouldShowDiff {
		shouldShowDiff = cfg.ShowDiff
	}
	if shouldShowDiff {
		diff := generateDiff(args.OldString, args.NewString)
		message.WriteString(fmt.Sprintf("\n%s\n", diff))
	}

	if backedUp {
		message.WriteString(fmt.Sprintf("\nBACKUP: Backup created: %s.bak", args.Path))
	}

	return map[string]any{
		"message":      message.String(),
		"path":         args.Path,
		"replacements": replacementCount,
		"replace_all":  args.ReplaceAll,
		"backed_up":    backedUp,
		"old_length":   len(args.OldString),
		"new_length":   len(args.NewString),
		"size_change":  len(newContent) - len(originalContent),
	}, nil
}

func generateDiff(oldStr, newStr string) string {
	var diff strings.Builder

	diff.WriteString("CHANGES:\n")
	diff.WriteString(strings.Repeat("-", 60) + "\n")

	oldLines := strings.Split(oldStr, "\n")
	for _, line := range oldLines {
		if line != "" {
			diff.WriteString(fmt.Sprintf("- %s\n", line))
		}
	}

	newLines := strings.Split(newStr, "\n")
	for _, line := range newLines {
		if line != "" {
			diff.WriteString(fmt.Sprintf("+ %s\n", line))
		}
	}

	diff.WriteString(strings.Repeat("-", 60))

	return diff.String()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
