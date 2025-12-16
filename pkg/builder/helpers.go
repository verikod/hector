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

package builder

import (
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// FunctionTool creates a callable tool from a typed Go function.
//
// This is a convenience wrapper around functiontool.New for ergonomic use.
// The function signature must be:
//
//	func(tool.Context, Args) (map[string]any, error)
//
// Where Args is a struct with json and jsonschema tags defining the parameters.
//
// Example:
//
//	type GetWeatherArgs struct {
//	    City string `json:"city" jsonschema:"required,description=City name"`
//	}
//
//	tool, err := builder.FunctionTool(
//	    "get_weather",
//	    "Get current weather for a city",
//	    func(ctx tool.Context, args GetWeatherArgs) (map[string]any, error) {
//	        return map[string]any{"temp": 22}, nil
//	    },
//	)
func FunctionTool[Args any](
	name string,
	description string,
	fn func(tool.Context, Args) (map[string]any, error),
) (tool.CallableTool, error) {
	return functiontool.New(functiontool.Config{
		Name:        name,
		Description: description,
	}, fn)
}

// MustFunctionTool creates a callable tool or panics on error.
//
// Use this only when you're certain the configuration is valid.
func MustFunctionTool[Args any](
	name string,
	description string,
	fn func(tool.Context, Args) (map[string]any, error),
) tool.CallableTool {
	t, err := FunctionTool(name, description, fn)
	if err != nil {
		panic("failed to create function tool: " + err.Error())
	}
	return t
}

// boolPtr returns a pointer to the given bool value.
//
//nolint:unused // Reserved for future use
func boolPtr(b bool) *bool {
	return &b
}
