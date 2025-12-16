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

// Package instruction provides instruction templating utilities for Hector v2.
//
// Instructions can contain placeholders that are resolved at runtime:
//
//	{variable}           - resolves from session state
//	{app:variable}       - resolves from app-scoped state
//	{user:variable}      - resolves from user-scoped state
//	{temp:variable}      - resolves from temp-scoped state
//	{artifact.filename}  - resolves artifact text content
//	{variable?}          - optional (empty string if not found)
//
// Example:
//
//	instruction := "Hello {user_name}, you are working on {app:project_name}."
//	resolved, err := instruction.InjectState(ctx, instruction)
package instruction

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/verikod/hector/pkg/agent"
)

// State key prefixes matching adk-go and session package.
const (
	PrefixApp  = "app:"
	PrefixUser = "user:"
	PrefixTemp = "temp:"
)

// placeholderRegex matches {variable}, {artifact.name}, {variable?}, etc.
// Matches one or more opening braces, content without braces, one or more closing braces.
var placeholderRegex = regexp.MustCompile(`{+[^{}]*}+`)

// Template represents an instruction template with placeholders.
type Template struct {
	raw string
}

// New creates a new instruction template.
func New(template string) *Template {
	return &Template{raw: template}
}

// Raw returns the raw template string.
func (t *Template) Raw() string {
	return t.raw
}

// Render resolves all placeholders in the template using the context.
func (t *Template) Render(ctx agent.ReadonlyContext) (string, error) {
	return InjectState(ctx, t.raw)
}

// InjectState populates values in an instruction template from context.
// This is the main entry point for template resolution, matching adk-go's pattern.
//
// Placeholder syntax:
//   - {variable_name} - resolves from session state
//   - {app:variable} - resolves from app-scoped state
//   - {user:variable} - resolves from user-scoped state
//   - {temp:variable} - resolves from temp-scoped state
//   - {artifact.filename} - resolves artifact text content
//   - {variable?} - optional (empty string if not found, no error)
//
// If a required placeholder cannot be resolved, an error is returned.
// Invalid placeholder names (not matching identifier rules) are left as-is.
func InjectState(ctx agent.ReadonlyContext, template string) (string, error) {
	if template == "" {
		return "", nil
	}

	var result strings.Builder
	lastIndex := 0
	matches := placeholderRegex.FindAllStringIndex(template, -1)

	for _, matchIndexes := range matches {
		startIndex, endIndex := matchIndexes[0], matchIndexes[1]

		// Append text between matches
		result.WriteString(template[lastIndex:startIndex])

		// Get replacement for the current match
		matchStr := template[startIndex:endIndex]
		replacement, err := replaceMatch(ctx, matchStr)
		if err != nil {
			return "", err
		}
		result.WriteString(replacement)

		lastIndex = endIndex
	}

	// Append remaining text after the last match
	result.WriteString(template[lastIndex:])
	return result.String(), nil
}

// replaceMatch resolves a single placeholder match.
func replaceMatch(ctx agent.ReadonlyContext, match string) (string, error) {
	// Trim braces: "{var_name}" -> "var_name"
	varName := strings.TrimSpace(strings.Trim(match, "{}"))

	// Check for optional marker
	optional := false
	if strings.HasSuffix(varName, "?") {
		optional = true
		varName = strings.TrimSuffix(varName, "?")
	}

	// Handle artifact references: {artifact.filename}
	if after, ok := strings.CutPrefix(varName, "artifact."); ok {
		return resolveArtifact(ctx, after, optional)
	}

	// Validate state name
	if !isValidStateName(varName) {
		// Return original if not a valid identifier (treat as literal)
		return match, nil
	}

	// Resolve from state
	return resolveState(ctx, varName, optional)
}

// resolveArtifact loads artifact content by filename.
func resolveArtifact(ctx agent.ReadonlyContext, filename string, optional bool) (string, error) {
	if filename == "" {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("empty artifact filename")
	}

	// Get artifacts from context if available (requires CallbackContext)
	cbCtx, ok := ctx.(agent.CallbackContext)
	if !ok {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("artifacts not available in readonly context")
	}

	artifacts := cbCtx.Artifacts()
	if artifacts == nil {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("artifact service not available")
	}

	resp, err := artifacts.Load(ctx, filename)
	if err != nil {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("failed to load artifact %q: %w", filename, err)
	}

	// Extract text from the artifact part
	return extractTextFromPart(resp.Part), nil
}

// extractTextFromPart extracts text content from an a2a.Part.
func extractTextFromPart(part any) string {
	if part == nil {
		return ""
	}

	// Try common text extraction patterns
	switch p := part.(type) {
	case interface{ GetText() string }:
		return p.GetText()
	case interface{ Text() string }:
		return p.Text()
	case fmt.Stringer:
		return p.String()
	}

	// Try to extract from struct with Text field
	if textPart, ok := part.(struct{ Text string }); ok {
		return textPart.Text
	}

	return ""
}

// resolveState resolves a variable from session state.
func resolveState(ctx agent.ReadonlyContext, varName string, optional bool) (string, error) {
	state := ctx.ReadonlyState()
	if state == nil {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("session state not available")
	}

	value, err := state.Get(varName)
	if err != nil {
		if optional {
			return "", nil
		}
		return "", fmt.Errorf("state key %q: %w", varName, err)
	}

	if value == nil {
		if optional {
			return "", nil
		}
		return "", nil
	}

	return fmt.Sprintf("%v", value), nil
}

// isValidStateName checks if the variable name is a valid state name.
// Valid names are identifiers or prefixed identifiers (app:name, user:name, temp:name).
func isValidStateName(varName string) bool {
	parts := strings.Split(varName, ":")
	if len(parts) == 1 {
		return isIdentifier(varName)
	}

	if len(parts) == 2 {
		prefix := parts[0] + ":"
		validPrefixes := []string{PrefixApp, PrefixUser, PrefixTemp}
		if slices.Contains(validPrefixes, prefix) {
			return isIdentifier(parts[1])
		}
	}
	return false
}

// isIdentifier checks if a string is a valid identifier.
// Valid identifiers start with a letter or underscore, followed by letters, digits, or underscores.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
		} else {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

// MustInjectState is like InjectState but panics on error.
// Use only when you're certain the template is valid.
func MustInjectState(ctx agent.ReadonlyContext, template string) string {
	result, err := InjectState(ctx, template)
	if err != nil {
		panic(fmt.Sprintf("instruction.MustInjectState: %v", err))
	}
	return result
}

// HasPlaceholders returns true if the template contains any placeholders.
func HasPlaceholders(template string) bool {
	return placeholderRegex.MatchString(template)
}

// ListPlaceholders returns all placeholder names found in the template.
func ListPlaceholders(template string) []string {
	matches := placeholderRegex.FindAllString(template, -1)
	var names []string
	seen := make(map[string]bool)

	for _, match := range matches {
		name := strings.TrimSpace(strings.Trim(match, "{}"))
		name = strings.TrimSuffix(name, "?")
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names
}
