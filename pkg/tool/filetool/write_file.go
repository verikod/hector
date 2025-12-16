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

// WriteFileArgs defines the parameters for writing a file.
type WriteFileArgs struct {
	Path    string `json:"path" jsonschema:"required,description=File path relative to working directory"`
	Content string `json:"content" jsonschema:"required,description=Content to write to the file"`
	Backup  bool   `json:"backup,omitempty" jsonschema:"description=Create .bak backup if file exists,default=true"`
}

// WriteFileConfig defines configuration for the write_file tool.
type WriteFileConfig struct {
	MaxFileSize       int
	AllowedExtensions []string
	DeniedExtensions  []string
	BackupOnOverwrite bool
	WorkingDirectory  string
}

// NewWriteFile creates a new write_file tool using FunctionTool.
func NewWriteFile(cfg *WriteFileConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &WriteFileConfig{
			MaxFileSize:       1048576, // 1MB default
			BackupOnOverwrite: true,
			WorkingDirectory:  "./",
		}
	}

	if cfg.MaxFileSize == 0 {
		cfg.MaxFileSize = 1048576
	}
	if cfg.WorkingDirectory == "" {
		cfg.WorkingDirectory = "./"
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "write_file",
			Description: "Create a new file or completely overwrite an existing one. Use this tool ONLY when creating new files or when you intend to replace the ENTIRE content of a file. For modifying existing files, prefer search_replace or apply_patch.",
		},
		func(ctx tool.Context, args WriteFileArgs) (map[string]any, error) {
			return writeFileImpl(cfg, args)
		},
		func(args WriteFileArgs) error {
			// Validate path
			if err := validateWritePath(cfg, args.Path); err != nil {
				return err
			}

			// Validate content size
			if len(args.Content) > cfg.MaxFileSize {
				return fmt.Errorf("content too large: %d bytes (max: %d)", len(args.Content), cfg.MaxFileSize)
			}

			return nil
		},
	)
}

func writeFileImpl(cfg *WriteFileConfig, args WriteFileArgs) (map[string]any, error) {
	fullPath := filepath.Join(cfg.WorkingDirectory, args.Path)

	// Check if file exists for backup
	fileExisted := false
	if _, err := os.Stat(fullPath); err == nil {
		fileExisted = true

		// Create backup if requested and enabled
		if args.Backup && cfg.BackupOnOverwrite {
			backupPath := fullPath + ".bak"
			if err := copyFile(fullPath, backupPath); err != nil {
				return nil, fmt.Errorf("failed to create backup: %w", err)
			}
		}
	}

	// Create parent directory if needed
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Write file
	if err := os.WriteFile(fullPath, []byte(args.Content), 0644); err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}

	action := "created"
	if fileExisted {
		action = "overwritten"
	}

	message := fmt.Sprintf("File %s successfully: %s (%d bytes)", action, args.Path, len(args.Content))
	if fileExisted && args.Backup {
		message += fmt.Sprintf("\nBackup created: %s.bak", args.Path)
	}

	return map[string]any{
		"message":      message,
		"path":         args.Path,
		"size":         len(args.Content),
		"backed_up":    fileExisted && args.Backup,
		"file_existed": fileExisted,
		"action":       action,
	}, nil
}

func validateWritePath(cfg *WriteFileConfig, path string) error {
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
	absPath, err := filepath.Abs(filepath.Join(cfg.WorkingDirectory, cleaned))
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	absWorkDir, err := filepath.Abs(cfg.WorkingDirectory)
	if err != nil {
		return fmt.Errorf("invalid working directory: %w", err)
	}

	if !strings.HasPrefix(absPath, absWorkDir) {
		return fmt.Errorf("path escapes working directory")
	}

	// Check extension restrictions
	ext := filepath.Ext(path)

	// Check denied extensions first
	if len(cfg.DeniedExtensions) > 0 {
		for _, deniedExt := range cfg.DeniedExtensions {
			if ext == deniedExt || (ext == "" && deniedExt == "") {
				if ext == "" {
					return fmt.Errorf("extensionless files are explicitly denied")
				}
				return fmt.Errorf("file extension %s is explicitly denied", ext)
			}
		}
	}

	// Check allowed extensions
	if len(cfg.AllowedExtensions) > 0 {
		allowed := false
		for _, allowedExt := range cfg.AllowedExtensions {
			if ext == allowedExt {
				allowed = true
				break
			}
		}
		if !allowed {
			if ext == "" {
				return fmt.Errorf("extensionless files not allowed (add '' to allowed_extensions to allow Makefile, Dockerfile, etc.)")
			}
			return fmt.Errorf("file extension %s not allowed (allowed: %v)", ext, cfg.AllowedExtensions)
		}
	}

	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}
