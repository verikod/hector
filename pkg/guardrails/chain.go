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

package guardrails

import (
	"context"
)

// ChainMode determines how a chain handles multiple guardrails.
type ChainMode string

const (
	// ChainModeFailFast stops at the first blocking result.
	ChainModeFailFast ChainMode = "fail_fast"
	// ChainModeCollectAll runs all guardrails and collects all violations.
	ChainModeCollectAll ChainMode = "collect_all"
)

// InputChain runs multiple input guardrails in sequence.
type InputChain struct {
	guardrails []InputGuardrail
	mode       ChainMode
}

// NewInputChain creates a new input guardrail chain.
func NewInputChain(guardrails ...InputGuardrail) *InputChain {
	return &InputChain{
		guardrails: guardrails,
		mode:       ChainModeFailFast,
	}
}

// WithMode sets the chain mode.
func (c *InputChain) WithMode(mode ChainMode) *InputChain {
	c.mode = mode
	return c
}

// Add appends guardrails to the chain.
func (c *InputChain) Add(guardrails ...InputGuardrail) *InputChain {
	c.guardrails = append(c.guardrails, guardrails...)
	return c
}

// Guardrails returns the list of guardrails in the chain.
func (c *InputChain) Guardrails() []InputGuardrail {
	return c.guardrails
}

// Check runs all guardrails in the chain and returns the combined result.
// If mode is FailFast, it stops at the first blocking result.
// If mode is CollectAll, it runs all guardrails and returns all violations.
func (c *InputChain) Check(ctx context.Context, input string) (*Result, error) {
	var chainError *ChainError
	currentInput := input

	for _, g := range c.guardrails {
		result, err := g.Check(ctx, currentInput)
		if err != nil {
			return nil, err
		}

		if result.IsBlocking() {
			if c.mode == ChainModeFailFast {
				return result, nil
			}
			// CollectAll mode: collect the error
			if chainError == nil {
				chainError = &ChainError{}
			}
			chainError.Add(NewGuardrailError(result))
			continue
		}

		// Handle modifications
		if result.Action == ActionModify {
			if modified, ok := result.Modified.(string); ok {
				currentInput = modified
			}
		}
	}

	// If we collected any errors, return a blocking result
	if chainError != nil && chainError.HasErrors() {
		return &Result{
			Action:   ActionBlock,
			Severity: SeverityHigh,
			Reason:   chainError.Error(),
			Details: map[string]any{
				"violations": len(chainError.Errors),
			},
		}, nil
	}

	// All guardrails passed
	if currentInput != input {
		return Modify("input_chain", currentInput, "input modified by chain"), nil
	}
	return Allow("input_chain"), nil
}

// OutputChain runs multiple output guardrails in sequence.
type OutputChain struct {
	guardrails []OutputGuardrail
	mode       ChainMode
}

// NewOutputChain creates a new output guardrail chain.
func NewOutputChain(guardrails ...OutputGuardrail) *OutputChain {
	return &OutputChain{
		guardrails: guardrails,
		mode:       ChainModeFailFast,
	}
}

// WithMode sets the chain mode.
func (c *OutputChain) WithMode(mode ChainMode) *OutputChain {
	c.mode = mode
	return c
}

// Add appends guardrails to the chain.
func (c *OutputChain) Add(guardrails ...OutputGuardrail) *OutputChain {
	c.guardrails = append(c.guardrails, guardrails...)
	return c
}

// Guardrails returns the list of guardrails in the chain.
func (c *OutputChain) Guardrails() []OutputGuardrail {
	return c.guardrails
}

// Check runs all guardrails in the chain and returns the combined result.
func (c *OutputChain) Check(ctx context.Context, output string) (*Result, error) {
	var chainError *ChainError
	currentOutput := output

	for _, g := range c.guardrails {
		result, err := g.Check(ctx, currentOutput)
		if err != nil {
			return nil, err
		}

		if result.IsBlocking() {
			if c.mode == ChainModeFailFast {
				return result, nil
			}
			if chainError == nil {
				chainError = &ChainError{}
			}
			chainError.Add(NewGuardrailError(result))
			continue
		}

		if result.Action == ActionModify {
			if modified, ok := result.Modified.(string); ok {
				currentOutput = modified
			}
		}
	}

	if chainError != nil && chainError.HasErrors() {
		return &Result{
			Action:   ActionBlock,
			Severity: SeverityHigh,
			Reason:   chainError.Error(),
			Details: map[string]any{
				"violations": len(chainError.Errors),
			},
		}, nil
	}

	if currentOutput != output {
		return Modify("output_chain", currentOutput, "output modified by chain"), nil
	}
	return Allow("output_chain"), nil
}

// ToolChain runs multiple tool guardrails in sequence.
type ToolChain struct {
	guardrails []ToolGuardrail
	mode       ChainMode
}

// NewToolChain creates a new tool guardrail chain.
func NewToolChain(guardrails ...ToolGuardrail) *ToolChain {
	return &ToolChain{
		guardrails: guardrails,
		mode:       ChainModeFailFast,
	}
}

// WithMode sets the chain mode.
func (c *ToolChain) WithMode(mode ChainMode) *ToolChain {
	c.mode = mode
	return c
}

// Add appends guardrails to the chain.
func (c *ToolChain) Add(guardrails ...ToolGuardrail) *ToolChain {
	c.guardrails = append(c.guardrails, guardrails...)
	return c
}

// Guardrails returns the list of guardrails in the chain.
func (c *ToolChain) Guardrails() []ToolGuardrail {
	return c.guardrails
}

// Check runs all guardrails in the chain and returns the combined result.
func (c *ToolChain) Check(ctx context.Context, toolName string, args map[string]any) (*Result, error) {
	var chainError *ChainError
	currentArgs := args
	argsModified := false

	for _, g := range c.guardrails {
		result, err := g.Check(ctx, toolName, currentArgs)
		if err != nil {
			return nil, err
		}

		if result.IsBlocking() {
			if c.mode == ChainModeFailFast {
				return result, nil
			}
			if chainError == nil {
				chainError = &ChainError{}
			}
			chainError.Add(NewGuardrailError(result))
			continue
		}

		if result.Action == ActionModify {
			if modified, ok := result.Modified.(map[string]any); ok {
				currentArgs = modified
				argsModified = true
			}
		}
	}

	if chainError != nil && chainError.HasErrors() {
		return &Result{
			Action:   ActionBlock,
			Severity: SeverityHigh,
			Reason:   chainError.Error(),
			Details: map[string]any{
				"violations": len(chainError.Errors),
			},
		}, nil
	}

	if argsModified {
		return Modify("tool_chain", currentArgs, "args modified by chain"), nil
	}
	return Allow("tool_chain"), nil
}
