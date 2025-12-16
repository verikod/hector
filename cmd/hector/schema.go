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

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/invopop/jsonschema"
	"github.com/verikod/hector/pkg/config"
)

// SchemaCmd generates JSON Schema from Hector config structs.
// This schema is used by the web UI config builder to auto-generate forms.
// Output is written to stdout for flexibility (can be redirected in Makefile).
type SchemaCmd struct {
	// Compact enables compact JSON output (no indentation)
	Compact bool `short:"c" help:"Compact JSON output (no indentation)."`
}

// Run executes the schema generation command.
func (c *SchemaCmd) Run(cli *CLI) error {
	// Create reflector with appropriate settings
	reflector := &jsonschema.Reflector{
		// Disallow additional properties for strict validation
		AllowAdditionalProperties: false,
		// Inline all definitions (no $ref) for @rjsf/core compatibility
		DoNotReference: true,
	}

	// Generate schema from Config struct
	schema := reflector.Reflect(&config.Config{})

	// Add metadata
	schema.ID = "https://hector.dev/schemas/config.json"
	schema.Title = "Hector Configuration Schema"
	schema.Description = "Complete configuration schema for Hector v2 agent framework"

	// Add schema version
	schema.Version = "http://json-schema.org/draft-07/schema#"

	// Add examples (helpful for documentation and testing)
	schema.Examples = []interface{}{
		map[string]interface{}{
			"version": "2",
			"name":    "my-assistant",
			"llms": map[string]interface{}{
				"default": map[string]interface{}{
					"provider": "anthropic",
					"model":    "claude-sonnet-4-20250514",
					"api_key":  "${ANTHROPIC_API_KEY}",
				},
			},
			"agents": map[string]interface{}{
				"assistant": map[string]interface{}{
					"llm":         "default",
					"instruction": "You are a helpful assistant.",
					"tools":       []string{"search"},
				},
			},
		},
	}

	// Marshal to JSON and write to stdout
	encoder := json.NewEncoder(os.Stdout)
	if !c.Compact {
		encoder.SetIndent("", "  ")
	}

	if err := encoder.Encode(schema); err != nil {
		return fmt.Errorf("failed to encode schema: %w", err)
	}

	return nil
}
