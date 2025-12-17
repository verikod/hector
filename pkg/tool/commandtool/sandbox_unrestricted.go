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

//go:build unrestricted

// Package commandtool provides a secure, streaming command execution tool.
//
// # UNRESTRICTED BUILD
//
// This file is only included when building with -tags=unrestricted.
// In this mode, SandboxEnforced = false, which means:
//   - Config can completely override DefaultDeniedCommands
//   - Config can completely override DefaultDeniedPatterns
//   - The operator takes full responsibility for security
//
// WARNING: Only use this in controlled environments where you explicitly
// need to allow commands that are blocked by default (rm, sudo, etc.).
package commandtool

// SandboxEnforced is false in unrestricted builds.
// This allows config to fully override security defaults.
const SandboxEnforced = false
