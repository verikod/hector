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
	"github.com/kadirpekel/hector/pkg/guardrails"
)

// DefaultBuilders returns the standard tool guardrail builders.
// Use with Config.BuildToolChain(tool.DefaultBuilders()).
func DefaultBuilders() guardrails.ToolChainBuilders {
	return guardrails.ToolChainBuilders{
		Authorizer: BuildAuthorizer,
	}
}

// BuildAuthorizer creates an Authorizer from config.
func BuildAuthorizer(cfg *guardrails.AuthorizationConfig) guardrails.ToolGuardrail {
	a := NewAuthorizer()
	if len(cfg.AllowedTools) > 0 {
		a.AllowOnly(cfg.AllowedTools...)
	}
	if len(cfg.BlockedTools) > 0 {
		a.Block(cfg.BlockedTools...)
	}
	if cfg.Action != "" {
		a.WithAction(cfg.Action)
	}
	if cfg.Severity != "" {
		a.WithSeverity(cfg.Severity)
	}
	return a
}
