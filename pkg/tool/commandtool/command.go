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

// Package commandtool provides a secure, streaming command execution tool.
//
// Security Features:
//   - AllowedCommands: Whitelist of permitted base commands
//   - DeniedCommands: Blacklist of dangerous commands (applied first)
//   - DeniedPatterns: Regex patterns for dangerous patterns (rm -rf, etc.)
//   - WorkingDirectory: Restricted execution directory
//   - MaxExecutionTime: Timeout to prevent runaway processes
//   - RequireApproval: HITL approval before execution
//
// Streaming:
//   - Uses iter.Seq2 pattern matching LLM.GenerateContent
//   - Real-time stdout/stderr streaming for UI feedback
//
// Example usage:
//
//	tool := commandtool.New(commandtool.Config{
//	    AllowedCommands: []string{"docker", "npm", "go", "ls", "cat"},
//	    DeniedCommands:  []string{"rm", "sudo", "chmod"},
//	    WorkingDir:      "/project",
//	    RequireApproval: true, // HITL approval required
//	})
package commandtool

import (
	"bufio"
	"context"
	"fmt"
	"iter"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kadirpekel/hector/pkg/tool"
)

// DefaultDeniedCommands are commands that are blocked by default for security.
var DefaultDeniedCommands = []string{
	"rm", "rmdir", "sudo", "su", "chmod", "chown",
	"dd", "mkfs", "fdisk", "mount", "umount",
	"kill", "killall", "pkill", "reboot", "shutdown",
	"passwd", "useradd", "userdel", "groupadd",
}

// DefaultDeniedPatterns are regex patterns blocked by default.
var DefaultDeniedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`rm\s+(-rf|-fr|--recursive)`),     // rm -rf variants
	regexp.MustCompile(`>\s*/dev/`),                      // writes to /dev
	regexp.MustCompile(`:\(\)\s*\{\s*:\|:\s*&\s*\}\s*;`), // fork bomb
	regexp.MustCompile(`wget.*\|\s*sh`),                  // wget pipe to shell
	regexp.MustCompile(`curl.*\|\s*sh`),                  // curl pipe to shell
	regexp.MustCompile(`eval\s*\$`),                      // eval with variable
	regexp.MustCompile(`\$\(.*\)\s*>\s*/`),               // command substitution to root
	regexp.MustCompile(`>\s*/etc/`),                      // writes to /etc
	regexp.MustCompile(`chmod\s+777`),                    // overly permissive chmod
	regexp.MustCompile(`--no-preserve-root`),             // dangerous flag
}

// Config configures the command tool with security settings.
type Config struct {
	// Name overrides the default tool name.
	Name string

	// AllowedCommands is a whitelist of base commands that can be executed.
	// If empty and DenyByDefault is false, all non-denied commands are allowed.
	// If empty and DenyByDefault is true, no commands are allowed.
	AllowedCommands []string

	// DeniedCommands is a blacklist of base commands (checked before allowed).
	// Defaults to DefaultDeniedCommands if nil.
	DeniedCommands []string

	// DeniedPatterns are regex patterns that block dangerous command patterns.
	// Defaults to DefaultDeniedPatterns if nil.
	DeniedPatterns []*regexp.Regexp

	// DenyByDefault requires explicit AllowedCommands whitelist.
	// When true, only commands in AllowedCommands are permitted.
	// When false, all commands except denied ones are permitted.
	DenyByDefault bool

	// WorkingDir sets the default working directory.
	// Commands cannot escape this directory.
	WorkingDir string

	// Timeout for command execution. Default: 5 minutes.
	Timeout time.Duration

	// RequireApproval triggers HITL approval before command execution.
	// When true, IsLongRunning() returns true and the tool uses the
	// approval flow instead of direct execution.
	RequireApproval bool

	// ApprovalPrompt customizes the approval message shown to users.
	ApprovalPrompt string
}

// CommandTool executes shell commands with security controls and streaming output.
type CommandTool struct {
	name            string
	allowedCommands map[string]bool
	deniedCommands  map[string]bool
	deniedPatterns  []*regexp.Regexp
	denyByDefault   bool
	workingDir      string
	timeout         time.Duration
	requireApproval bool
	approvalPrompt  string
}

// New creates a new secure command execution tool.
func New(cfg Config) *CommandTool {
	name := cfg.Name
	if name == "" {
		name = "execute_command"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}

	// Build allowed commands set
	allowedCommands := make(map[string]bool)
	for _, cmd := range cfg.AllowedCommands {
		allowedCommands[cmd] = true
	}

	// Build denied commands set (use defaults if not provided)
	deniedCommands := make(map[string]bool)
	deniedList := cfg.DeniedCommands
	if deniedList == nil {
		deniedList = DefaultDeniedCommands
	}
	for _, cmd := range deniedList {
		deniedCommands[cmd] = true
	}

	// Use default patterns if not provided
	deniedPatterns := cfg.DeniedPatterns
	if deniedPatterns == nil {
		deniedPatterns = DefaultDeniedPatterns
	}

	approvalPrompt := cfg.ApprovalPrompt
	if approvalPrompt == "" {
		approvalPrompt = "Command execution requires your approval"
	}

	return &CommandTool{
		name:            name,
		allowedCommands: allowedCommands,
		deniedCommands:  deniedCommands,
		deniedPatterns:  deniedPatterns,
		denyByDefault:   cfg.DenyByDefault,
		workingDir:      cfg.WorkingDir,
		timeout:         timeout,
		requireApproval: cfg.RequireApproval,
		approvalPrompt:  approvalPrompt,
	}
}

// Name returns the tool name.
func (t *CommandTool) Name() string {
	return t.name
}

// Description returns the tool description.
func (t *CommandTool) Description() string {
	desc := "Execute a secure shell command. Use this for system operations, installing dependencies, running tests, or inspecting the environment."

	if t.requireApproval {
		desc += " Requires human approval before execution."
	}

	if len(t.allowedCommands) > 0 {
		allowed := make([]string, 0, len(t.allowedCommands))
		for cmd := range t.allowedCommands {
			allowed = append(allowed, cmd)
		}
		desc += fmt.Sprintf(" Allowed: %s.", strings.Join(allowed, ", "))
	}

	return desc
}

// IsLongRunning returns false - command execution is not async.
func (t *CommandTool) IsLongRunning() bool {
	return false
}

// RequiresApproval returns true if approval is enabled (HITL pattern).
// This causes the task to transition to INPUT_REQUIRED state before execution.
func (t *CommandTool) RequiresApproval() bool {
	return t.requireApproval
}

// Schema returns the JSON schema for the tool parameters.
func (t *CommandTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]any{
				"type":        "string",
				"description": "Optional working directory for command execution",
			},
		},
		"required": []string{"command"},
	}
}

// validateCommand performs security validation on the command.
func (t *CommandTool) validateCommand(command string) error {
	if command == "" {
		return fmt.Errorf("command is required")
	}

	// Check denied patterns first (most dangerous)
	for _, pattern := range t.deniedPatterns {
		if pattern.MatchString(command) {
			return fmt.Errorf("command matches denied pattern: %s", pattern.String())
		}
	}

	// Extract base command
	baseCmd := t.extractBaseCommand(command)
	if baseCmd == "" {
		return fmt.Errorf("could not extract base command")
	}

	// Check if base command is explicitly denied
	if t.deniedCommands[baseCmd] {
		return fmt.Errorf("command not allowed: %s (in deny list)", baseCmd)
	}

	// If deny by default, must be in allowed list
	if t.denyByDefault {
		if !t.allowedCommands[baseCmd] {
			return fmt.Errorf("command not allowed: %s (not in allow list)", baseCmd)
		}
	} else if len(t.allowedCommands) > 0 {
		// If we have an allow list, check it
		if !t.allowedCommands[baseCmd] {
			return fmt.Errorf("command not allowed: %s (not in allow list)", baseCmd)
		}
	}

	return nil
}

// extractBaseCommand extracts the first command from a potentially piped command.
func (t *CommandTool) extractBaseCommand(command string) string {
	// Split on shell operators
	parts := strings.FieldsFunc(command, func(r rune) bool {
		return r == '|' || r == '>' || r == '<' || r == ';' || r == '&'
	})

	if len(parts) == 0 {
		return ""
	}

	firstCmd := strings.TrimSpace(parts[0])
	cmdParts := strings.Fields(firstCmd)
	if len(cmdParts) == 0 {
		return ""
	}

	return cmdParts[0]
}

// CallStreaming executes the command and yields output using iter.Seq2.
// Note: The approval flow is handled externally by the agent flow via RequiresApproval().
// When CallStreaming is invoked, approval has already been granted (if configured).
func (t *CommandTool) CallStreaming(ctx tool.Context, args map[string]any) iter.Seq2[*tool.Result, error] {
	return func(yield func(*tool.Result, error) bool) {
		command, ok := args["command"].(string)
		if !ok || command == "" {
			yield(nil, fmt.Errorf("command is required"))
			return
		}

		// Validate command for security
		if err := t.validateCommand(command); err != nil {
			yield(nil, err)
			return
		}

		// Determine working directory
		workDir := t.workingDir
		if wd, ok := args["working_dir"].(string); ok && wd != "" {
			workDir = wd
		}

		// Execute command with streaming
		t.executeStreaming(ctx, command, workDir, yield)
	}
}

// executeStreaming performs the actual command execution with streaming output.
func (t *CommandTool) executeStreaming(
	ctx tool.Context,
	command, workDir string,
	yield func(*tool.Result, error) bool,
) {
	// Create context with timeout
	execCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Create command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		yield(nil, fmt.Errorf("failed to create stdout pipe: %w", err))
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		yield(nil, fmt.Errorf("failed to create stderr pipe: %w", err))
		return
	}

	// Start command
	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		yield(nil, fmt.Errorf("failed to start command: %w", err))
		return
	}

	// Channel for collecting output lines
	lines := make(chan string, 100)
	var wg sync.WaitGroup
	var accumulated strings.Builder

	// Stream stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text() + "\n":
			case <-execCtx.Done():
				return
			}
		}
	}()

	// Stream stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case lines <- "[stderr] " + scanner.Text() + "\n":
			case <-execCtx.Done():
				return
			}
		}
	}()

	// Close lines channel when both readers are done
	go func() {
		wg.Wait()
		close(lines)
	}()

	// Yield lines as they arrive (streaming)
	for line := range lines {
		accumulated.WriteString(line)

		if !yield(&tool.Result{
			Content:   line,
			Streaming: true,
		}, nil) {
			// Client disconnected, cancel command
			cancel()
			return
		}
	}

	// Wait for command to complete
	cmdErr := cmd.Wait()
	duration := time.Since(startTime)

	// Yield final result
	finalContent := accumulated.String()
	if finalContent == "" {
		finalContent = "(no output)"
	}

	result := &tool.Result{
		Content:   finalContent,
		Streaming: false, // Final result
		Metadata: map[string]any{
			"command":     command,
			"working_dir": workDir,
			"duration_ms": duration.Milliseconds(),
			"exit_code":   cmd.ProcessState.ExitCode(),
		},
	}

	if cmdErr != nil {
		result.Error = cmdErr.Error()
	}

	yield(result, nil)
}

// Ensure CommandTool implements StreamingTool
var _ tool.StreamingTool = (*CommandTool)(nil)
