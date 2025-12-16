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

package functiontool_test

import (
	"fmt"

	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// Example_basic demonstrates basic function tool usage
func Example_basic() {
	type GetWeatherArgs struct {
		City  string `json:"city" jsonschema:"required,description=City name"`
		Units string `json:"units,omitempty" jsonschema:"description=Temperature units,default=celsius,enum=celsius|fahrenheit"`
	}

	weatherTool, err := functiontool.New(
		functiontool.Config{
			Name:        "get_weather",
			Description: "Get current weather for a city",
		},
		func(ctx tool.Context, args GetWeatherArgs) (map[string]any, error) {
			// Simulate weather API call
			return map[string]any{
				"city":      args.City,
				"temp":      22,
				"condition": "sunny",
				"units":     args.Units,
			}, nil
		},
	)

	if err != nil {
		panic(err)
	}

	fmt.Printf("Tool Name: %s\n", weatherTool.Name())
	fmt.Printf("Is Long Running: %v\n", weatherTool.IsLongRunning())
	// Output:
	// Tool Name: get_weather
	// Is Long Running: false
}

// Example_withValidation demonstrates custom validation
func Example_withValidation() {
	type CreateFileArgs struct {
		Path    string `json:"path" jsonschema:"required,description=File path"`
		Content string `json:"content" jsonschema:"required,description=File content"`
	}

	createFileTool, err := functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "create_file",
			Description: "Create a new file",
		},
		func(ctx tool.Context, args CreateFileArgs) (map[string]any, error) {
			// Simulate file creation
			return map[string]any{
				"path":  args.Path,
				"bytes": len(args.Content),
			}, nil
		},
		func(args CreateFileArgs) error {
			// Custom validation
			if len(args.Content) > 1000000 {
				return fmt.Errorf("content too large: %d bytes", len(args.Content))
			}
			return nil
		},
	)

	if err != nil {
		panic(err)
	}

	fmt.Printf("Tool: %s\n", createFileTool.Name())
	// Output:
	// Tool: create_file
}

// Example_complexTypes demonstrates complex parameter types
func Example_complexTypes() {
	type SearchArgs struct {
		Query     string   `json:"query" jsonschema:"required,description=Search query"`
		Languages []string `json:"languages,omitempty" jsonschema:"description=Language filters"`
		MaxCount  int      `json:"max_count,omitempty" jsonschema:"description=Max results,default=10,minimum=1,maximum=100"`
		Type      string   `json:"type,omitempty" jsonschema:"description=Search type,enum=semantic|keyword"`
	}

	searchTool, err := functiontool.New(
		functiontool.Config{
			Name:        "search",
			Description: "Search documents with filters",
		},
		func(ctx tool.Context, args SearchArgs) (map[string]any, error) {
			return map[string]any{
				"query":   args.Query,
				"results": []string{"doc1", "doc2"},
				"count":   2,
			}, nil
		},
	)

	if err != nil {
		panic(err)
	}

	schema := searchTool.Schema()
	fmt.Printf("Schema type: %s\n", schema["type"])
	// Output:
	// Schema type: object
}
