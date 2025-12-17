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

//go:build !unrestricted

// Package commandtool provides a secure, streaming command execution tool.
//
// Sandbox Enforcement:
//
// By default, Hector is compiled with SandboxEnforced = true, which means:
//   - DefaultDeniedCommands are ALWAYS applied (cannot be emptied via config)
//   - DefaultDeniedPatterns are ALWAYS applied (cannot be removed via config)
//   - Config can ADD to deny lists, but not remove default protections
//
// To compile an unrestricted version (for advanced users who understand the risks):
//
//	go build -tags=unrestricted ./cmd/hector
//
// This should only be used in controlled environments where the operator
// explicitly wants to allow all commands.
package commandtool

// SandboxEnforced indicates whether sandbox protections are permanently enabled.
// When true (the default), DefaultDeniedCommands and DefaultDeniedPatterns
// cannot be bypassed via configuration.
const SandboxEnforced = true
