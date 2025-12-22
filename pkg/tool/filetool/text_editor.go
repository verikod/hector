// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel

package filetool

import (
	"fmt"

	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// TextEditorArgs defines the parameters for the standard text_editor tool.
// Schema reference: https://docs.anthropic.com/en/docs/build-with-claude/computer-use#text-editor
type TextEditorArgs struct {
	Command    string `json:"command" jsonschema:"required,enum=view,enum=create,enum=str_replace,enum=insert,enum=undo_edit,description=The file operation to perform"`
	Path       string `json:"path" jsonschema:"required,description=Path to the file to operate on"`
	FileText   string `json:"file_text,omitempty" jsonschema:"description=Content for create command"`
	OldStr     string `json:"old_str,omitempty" jsonschema:"description=Text to replace for str_replace command"`
	NewStr     string `json:"new_str,omitempty" jsonschema:"description=Replacement text for str_replace command"`
	InsertLine int    `json:"insert_line,omitempty" jsonschema:"description=Line number to insert after for insert command"`
	ViewRange  []int  `json:"view_range,omitempty" jsonschema:"description=Optional [start_line, end_line] for view command"`
}

// TextEditorConfig defines configuration for the text_editor tool.
type TextEditorConfig struct {
	MaxFileSize       int64
	AllowedExtensions []string
	DeniedExtensions  []string
	BackupOnOverwrite bool
	WorkingDirectory  string
}

// NewTextEditor creates a new text_editor tool using FunctionTool.
func NewTextEditor(cfg *TextEditorConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &TextEditorConfig{
			MaxFileSize:       10485760, // 10MB
			BackupOnOverwrite: true,
			WorkingDirectory:  "./",
		}
	}

	// Reuse existing config structs for internal calls
	readCfg := &ReadFileConfig{
		MaxFileSize:      cfg.MaxFileSize,
		WorkingDirectory: cfg.WorkingDirectory,
	}
	writeCfg := &WriteFileConfig{
		MaxFileSize:       int(cfg.MaxFileSize),
		AllowedExtensions: cfg.AllowedExtensions,
		DeniedExtensions:  cfg.DeniedExtensions,
		BackupOnOverwrite: cfg.BackupOnOverwrite,
		WorkingDirectory:  cfg.WorkingDirectory,
	}
	srCfg := &SearchReplaceConfig{
		MaxReplacements:  100, // Default
		CreateBackup:     cfg.BackupOnOverwrite,
		WorkingDirectory: cfg.WorkingDirectory,
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "text_editor",
			Description: "View and modify files. This tool is a multi-purpose file editor that supports viewing, creating, replacing text, and inserting text.",
		},
		func(ctx tool.Context, args TextEditorArgs) (map[string]any, error) {
			switch args.Command {
			case "view":
				// Map TextEditorArgs -> ReadFileArgs
				readArgs := ReadFileArgs{
					Path:        args.Path,
					LineNumbers: true, // Default to true for text_editor
				}
				if len(args.ViewRange) >= 1 {
					readArgs.StartLine = args.ViewRange[0]
				}
				if len(args.ViewRange) >= 2 {
					readArgs.EndLine = args.ViewRange[1]
				}
				return readFileImpl(readCfg, readArgs)

			case "create":
				// Map TextEditorArgs -> WriteFileArgs
				writeArgs := WriteFileArgs{
					Path:    args.Path,
					Content: args.FileText,
					Backup:  true,
				}
				return writeFileImpl(writeCfg, writeArgs)

			case "str_replace":
				// Map TextEditorArgs -> SearchReplaceArgs
				srArgs := SearchReplaceArgs{
					Path:      args.Path,
					OldString: args.OldStr,
					NewString: args.NewStr,
				}
				return searchReplaceImpl(srCfg, srArgs)

			case "insert":
				return nil, fmt.Errorf("insert command not yet implemented")

			case "undo_edit":
				return nil, fmt.Errorf("undo_edit command not yet implemented")

			default:
				return nil, fmt.Errorf("unknown command: %s", args.Command)
			}
		},
		func(args TextEditorArgs) error {
			// Basic validation before dispatch
			if args.Path == "" {
				return fmt.Errorf("path is required")
			}
			// Command specific validation could go here, or rely on implementations
			return nil
		},
	)
}
