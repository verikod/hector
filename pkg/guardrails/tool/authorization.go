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

package tool

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/verikod/hector/pkg/guardrails"
)

// Authorizer checks if tool calls are allowed based on allowed/blocked lists.
type Authorizer struct {
	allowedTools []string
	blockedTools []string
	action       guardrails.Action
	severity     guardrails.Severity
}

// NewAuthorizer creates a new tool authorizer.
func NewAuthorizer() *Authorizer {
	return &Authorizer{
		action:   guardrails.ActionBlock,
		severity: guardrails.SeverityHigh,
	}
}

// AllowOnly sets the allowed tools (whitelist mode).
// Only these tools are allowed; all others are blocked.
// Supports glob patterns (*, ?).
func (a *Authorizer) AllowOnly(tools ...string) *Authorizer {
	a.allowedTools = append(a.allowedTools, tools...)
	return a
}

// Block adds tools to the blocklist.
// These tools are never allowed, even if in the allow list.
// Supports glob patterns (*, ?).
func (a *Authorizer) Block(tools ...string) *Authorizer {
	a.blockedTools = append(a.blockedTools, tools...)
	return a
}

// WithAction sets the action to take on rejection.
func (a *Authorizer) WithAction(action guardrails.Action) *Authorizer {
	a.action = action
	return a
}

// WithSeverity sets the severity of rejections.
func (a *Authorizer) WithSeverity(severity guardrails.Severity) *Authorizer {
	a.severity = severity
	return a
}

// Name returns the guardrail name.
func (a *Authorizer) Name() string {
	return "tool_authorizer"
}

// Check validates if the tool call is authorized.
func (a *Authorizer) Check(_ context.Context, toolName string, _ map[string]any) (*guardrails.Result, error) {
	// Check blocklist first (takes precedence)
	for _, pattern := range a.blockedTools {
		matched, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(toolName))
		if matched {
			return &guardrails.Result{
				Action:        a.action,
				Severity:      a.severity,
				Reason:        "tool is blocked",
				GuardrailName: a.Name(),
				Details: map[string]any{
					"tool":    toolName,
					"pattern": pattern,
					"list":    "blocked",
				},
			}, nil
		}
	}

	// If allowlist is defined, tool must match
	if len(a.allowedTools) > 0 {
		allowed := false
		for _, pattern := range a.allowedTools {
			matched, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(toolName))
			if matched {
				allowed = true
				break
			}
		}
		if !allowed {
			return &guardrails.Result{
				Action:        a.action,
				Severity:      a.severity,
				Reason:        "tool is not in allowed list",
				GuardrailName: a.Name(),
				Details: map[string]any{
					"tool":         toolName,
					"allowed_list": a.allowedTools,
				},
			}, nil
		}
	}

	return guardrails.Allow(a.Name()), nil
}

// ArgumentValidator validates tool arguments.
type ArgumentValidator struct {
	requiredArgs map[string][]string // toolName -> required arg names
	blockedArgs  map[string][]string // toolName -> blocked arg names
	action       guardrails.Action
	severity     guardrails.Severity
}

// NewArgumentValidator creates a new argument validator.
func NewArgumentValidator() *ArgumentValidator {
	return &ArgumentValidator{
		requiredArgs: make(map[string][]string),
		blockedArgs:  make(map[string][]string),
		action:       guardrails.ActionBlock,
		severity:     guardrails.SeverityMedium,
	}
}

// RequireArgs requires specific arguments for a tool.
func (v *ArgumentValidator) RequireArgs(toolName string, args ...string) *ArgumentValidator {
	v.requiredArgs[toolName] = append(v.requiredArgs[toolName], args...)
	return v
}

// BlockArgs blocks specific arguments for a tool.
func (v *ArgumentValidator) BlockArgs(toolName string, args ...string) *ArgumentValidator {
	v.blockedArgs[toolName] = append(v.blockedArgs[toolName], args...)
	return v
}

// WithAction sets the action to take on violation.
func (v *ArgumentValidator) WithAction(action guardrails.Action) *ArgumentValidator {
	v.action = action
	return v
}

// WithSeverity sets the severity of violations.
func (v *ArgumentValidator) WithSeverity(severity guardrails.Severity) *ArgumentValidator {
	v.severity = severity
	return v
}

// Name returns the guardrail name.
func (v *ArgumentValidator) Name() string {
	return "argument_validator"
}

// Check validates tool arguments.
func (v *ArgumentValidator) Check(_ context.Context, toolName string, args map[string]any) (*guardrails.Result, error) {
	// Check blocked args
	if blocked, ok := v.blockedArgs[toolName]; ok {
		for _, argName := range blocked {
			if _, exists := args[argName]; exists {
				return &guardrails.Result{
					Action:        v.action,
					Severity:      v.severity,
					Reason:        "blocked argument provided",
					GuardrailName: v.Name(),
					Details: map[string]any{
						"tool":     toolName,
						"argument": argName,
					},
				}, nil
			}
		}
	}

	// Check required args
	if required, ok := v.requiredArgs[toolName]; ok {
		for _, argName := range required {
			if _, exists := args[argName]; !exists {
				return &guardrails.Result{
					Action:        v.action,
					Severity:      v.severity,
					Reason:        "required argument missing",
					GuardrailName: v.Name(),
					Details: map[string]any{
						"tool":     toolName,
						"argument": argName,
					},
				}, nil
			}
		}
	}

	return guardrails.Allow(v.Name()), nil
}

// Ensure interface compliance.
var (
	_ guardrails.ToolGuardrail = (*Authorizer)(nil)
	_ guardrails.ToolGuardrail = (*ArgumentValidator)(nil)
)
