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

package config

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads environment variables from .env files.
//
// Search order (first found wins):
//  1. Explicit path if provided
//  2. .env in current directory
//  3. .env in config file's directory (if config path provided)
//  4. .env in home directory (~/.env)
//
// This function is idempotent and safe to call multiple times.
// Existing environment variables are NOT overwritten.
func LoadDotEnv(paths ...string) error {
	// Try explicit paths first
	for _, path := range paths {
		if path != "" {
			if err := loadIfExists(path); err != nil {
				return err
			}
		}
	}

	// Try .env in current directory
	if err := loadIfExists(".env"); err != nil {
		return err
	}

	// Try .env in home directory
	if home, err := os.UserHomeDir(); err == nil {
		if err := loadIfExists(filepath.Join(home, ".env")); err != nil {
			return err
		}
	}

	return nil
}

// LoadDotEnvForConfig loads .env from the config file's directory.
func LoadDotEnvForConfig(configPath string) error {
	if configPath == "" {
		return LoadDotEnv()
	}

	// Get absolute path of config
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return LoadDotEnv()
	}

	// Try .env in config's directory
	configDir := filepath.Dir(absPath)
	envPath := filepath.Join(configDir, ".env")

	return LoadDotEnv(envPath)
}

// loadIfExists loads a .env file if it exists.
// Does NOT overwrite existing environment variables.
func loadIfExists(path string) error {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // File doesn't exist, not an error
	}

	// Load without overwriting existing vars
	if err := godotenv.Load(path); err != nil {
		// Log but don't fail - .env is optional
		slog.Debug("Failed to load .env file", "path", path, "error", err)
		return nil
	}

	slog.Debug("Loaded environment from .env", "path", path)
	return nil
}

// MustLoadDotEnv loads .env files and panics on error.
// Use this in init() or main() where errors should be fatal.
func MustLoadDotEnv(paths ...string) {
	if err := LoadDotEnv(paths...); err != nil {
		panic("failed to load .env: " + err.Error())
	}
}

// ReloadDotEnv reloads environment variables from .env files.
// Unlike LoadDotEnv, this OVERWRITES existing environment variables.
// Use this for hot reload when .env files change.
func ReloadDotEnv(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := reloadIfExists(path); err != nil {
			return err
		}
	}
	return nil
}

// ReloadDotEnvForConfig reloads .env from the config file's directory.
// Unlike LoadDotEnvForConfig, this OVERWRITES existing environment variables.
func ReloadDotEnvForConfig(configPath string) error {
	if configPath == "" {
		return nil
	}

	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil
	}

	configDir := filepath.Dir(absPath)
	envPath := filepath.Join(configDir, ".env")

	return ReloadDotEnv(envPath)
}

// reloadIfExists reloads a .env file, overwriting existing variables.
func reloadIfExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}

	// Overload overwrites existing vars (unlike Load)
	if err := godotenv.Overload(path); err != nil {
		slog.Debug("Failed to reload .env file", "path", path, "error", err)
		return nil
	}

	slog.Info("Reloaded environment from .env", "path", path)
	return nil
}
