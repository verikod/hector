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

package utils

import (
	"encoding/json"
	"text/template"
	"time"
)

// TemplateFuncs returns common template helper functions used across Hector.
// These are useful for payload transformation in webhooks and notifications.
func TemplateFuncs() template.FuncMap {
	return template.FuncMap{
		// toJson converts a value to a compact JSON string.
		"toJson": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		// toJsonPretty converts a value to a pretty-printed JSON string.
		"toJsonPretty": func(v any) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
		},
		// now returns the current time in RFC3339 format.
		"now": func() string {
			return time.Now().Format(time.RFC3339)
		},
		// default returns the default value if the value is nil or empty.
		"default": func(def, val any) any {
			if val == nil || val == "" {
				return def
			}
			return val
		},
	}
}
