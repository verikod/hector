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
	"regexp"
	"strings"

	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// GrepSearchArgs defines the parameters for searching files.
type GrepSearchArgs struct {
	Pattern         string `json:"pattern" jsonschema:"required,description=Regular expression pattern to search for (supports Go regex syntax)"`
	Path            string `json:"path,omitempty" jsonschema:"description=File or directory path to search in,default=."`
	FilePattern     string `json:"file_pattern,omitempty" jsonschema:"description=File glob pattern to filter files (e.g. '*.go' '*.py')"`
	CaseInsensitive bool   `json:"case_insensitive,omitempty" jsonschema:"description=Perform case-insensitive search,default=false"`
	ContextLines    int    `json:"context_lines,omitempty" jsonschema:"description=Number of context lines to show before and after matches,default=2,minimum=0,maximum=10"`
	MaxResults      int    `json:"max_results,omitempty" jsonschema:"description=Maximum number of matches to return,default=100,minimum=1,maximum=1000"`
	Recursive       bool   `json:"recursive,omitempty" jsonschema:"description=Search recursively in directories,default=true"`
}

// GrepSearchConfig defines configuration for the grep_search tool.
type GrepSearchConfig struct {
	MaxResults       int
	MaxFileSize      int64
	WorkingDirectory string
	ContextLines     int
}

// NewGrepSearch creates a new grep_search tool using FunctionTool.
func NewGrepSearch(cfg *GrepSearchConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &GrepSearchConfig{
			MaxResults:       1000,
			MaxFileSize:      10485760, // 10MB
			WorkingDirectory: "./",
			ContextLines:     2,
		}
	}

	if cfg.MaxResults == 0 {
		cfg.MaxResults = 1000
	}
	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = 10485760
	}
	if cfg.WorkingDirectory == "" {
		cfg.WorkingDirectory = "./"
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "grep_search",
			Description: "Search for patterns in files using regular expressions. Like Unix grep but with context lines. Use for finding exact strings, symbols, or regex patterns across files.",
		},
		func(ctx tool.Context, args GrepSearchArgs) (map[string]any, error) {
			return grepSearchImpl(cfg, args)
		},
		func(args GrepSearchArgs) error {
			// Validate regex pattern
			pattern := args.Pattern
			if args.CaseInsensitive {
				pattern = "(?i)" + pattern
			}
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("invalid regex pattern: %w", err)
			}

			// Validate path if provided
			searchPath := args.Path
			if searchPath == "" {
				searchPath = "."
			}
			return validateSearchPath(cfg.WorkingDirectory, searchPath)
		},
	)
}

func grepSearchImpl(cfg *GrepSearchConfig, args GrepSearchArgs) (map[string]any, error) {
	// Default values
	searchPath := "."
	if args.Path != "" {
		searchPath = args.Path
	}

	contextLines := cfg.ContextLines
	if args.ContextLines > 0 {
		contextLines = args.ContextLines
	}

	maxResults := 100
	if args.MaxResults > 0 {
		maxResults = args.MaxResults
	}
	if maxResults > cfg.MaxResults {
		maxResults = cfg.MaxResults
	}

	// Default recursive to true per schema default and legacy behavior
	// The schema specifies default=true, so we default to true
	// Note: Go's zero value for bool is false, but schema default is true
	// Since JSON schema defaults are hints for the LLM (not auto-applied),
	// we need to handle defaults ourselves. We default to true and assume
	// false values are explicitly set (even though we can't distinguish from zero value)
	recursive := true
	// If Recursive is false, assume it was explicitly set (schema should ensure true when unset)
	// This matches legacy behavior where recursive defaults to true
	if !args.Recursive {
		recursive = false
	}

	// Compile regex
	pattern := args.Pattern
	if args.CaseInsensitive {
		pattern = "(?i)" + pattern
	}
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	// Get full path
	fullPath := filepath.Join(cfg.WorkingDirectory, searchPath)
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	// Find files to search
	var filesToSearch []string
	if fileInfo.IsDir() {
		if recursive {
			_ = filepath.Walk(fullPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil // skip errors
				}
				if !info.IsDir() && info.Size() <= cfg.MaxFileSize {
					if args.FilePattern == "" || matchesPattern(filepath.Base(path), args.FilePattern) {
						relPath, _ := filepath.Rel(cfg.WorkingDirectory, path)
						filesToSearch = append(filesToSearch, relPath)
					}
				}
				return nil
			})
		} else {
			entries, err := os.ReadDir(fullPath)
			if err == nil {
				for _, entry := range entries {
					if !entry.IsDir() {
						if info, err := entry.Info(); err == nil && info.Size() <= cfg.MaxFileSize {
							fileName := entry.Name()
							if args.FilePattern == "" || matchesPattern(fileName, args.FilePattern) {
								relPath := filepath.Join(searchPath, fileName)
								filesToSearch = append(filesToSearch, relPath)
							}
						}
					}
				}
			}
		}
	} else {
		filesToSearch = append(filesToSearch, searchPath)
	}

	// Search files
	results := []map[string]any{}
	totalMatches := 0

	for _, filePath := range filesToSearch {
		if totalMatches >= maxResults {
			break
		}

		matches, err := searchFile(cfg.WorkingDirectory, filePath, regex, contextLines)
		if err != nil {
			continue // skip files with errors
		}

		for _, match := range matches {
			if totalMatches >= maxResults {
				break
			}
			match["file"] = filePath
			results = append(results, match)
			totalMatches++
		}
	}

	// Build output
	var output strings.Builder
	output.WriteString(fmt.Sprintf("PATTERN: %s\n", args.Pattern))
	output.WriteString(fmt.Sprintf("SEARCH_PATH: %s\n", searchPath))
	output.WriteString(fmt.Sprintf("STATS: Found %d matches in %d files\n", totalMatches, len(results)))
	output.WriteString(strings.Repeat("─", 60) + "\n")

	if len(results) == 0 {
		output.WriteString("\nNo matches found.\n")
	} else {
		currentFile := ""
		for _, result := range results {
			file := result["file"].(string)
			lineNum := result["line"].(int)
			line := result["content"].(string)
			context := result["context"].([]string)

			if file != currentFile {
				if currentFile != "" {
					output.WriteString("\n")
				}
				output.WriteString(fmt.Sprintf("\nFILE: %s\n", file))
				currentFile = file
			}

			if len(context) > 0 {
				for _, ctx := range context {
					output.WriteString(fmt.Sprintf("  %s\n", ctx))
				}
			}

			output.WriteString(fmt.Sprintf("→ %d: %s\n", lineNum, line))
		}
	}

	if totalMatches >= maxResults {
		output.WriteString(fmt.Sprintf("\nWARN: Results limited to %d matches\n", maxResults))
	}

	return map[string]any{
		"content":          output.String(),
		"matches":          results,
		"pattern":          args.Pattern,
		"path":             searchPath,
		"total_matches":    totalMatches,
		"files_searched":   len(filesToSearch),
		"case_insensitive": args.CaseInsensitive,
		"recursive":        recursive,
		"truncated":        totalMatches >= maxResults,
	}, nil
}

func searchFile(workingDir, filePath string, regex *regexp.Regexp, contextLines int) ([]map[string]any, error) {
	fullPath := filepath.Join(workingDir, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	results := []map[string]any{}

	for i, line := range lines {
		if regex.MatchString(line) {
			context := []string{}

			// Add context before
			for j := contextLines; j > 0; j-- {
				if i-j >= 0 {
					context = append(context, fmt.Sprintf("%6d  %s", i-j+1, lines[i-j]))
				}
			}

			results = append(results, map[string]any{
				"line":    i + 1,
				"content": line,
				"context": context,
			})
		}
	}

	return results, nil
}

func matchesPattern(filename, pattern string) bool {
	matched, err := filepath.Match(pattern, filename)
	if err != nil {
		return false
	}
	return matched
}

func validateSearchPath(workingDir, path string) error {
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

	return nil
}
