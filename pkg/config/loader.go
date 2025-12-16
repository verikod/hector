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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/verikod/hector/pkg/config/provider"
	"gopkg.in/yaml.v3"
)

// Loader loads and watches configuration from a Provider.
type Loader struct {
	provider provider.Provider
	onChange func(*Config)
}

// LoaderOption configures a Loader.
type LoaderOption func(*Loader)

// WithOnChange sets a callback invoked when config changes.
func WithOnChange(fn func(*Config)) LoaderOption {
	return func(l *Loader) {
		l.onChange = fn
	}
}

// NewLoader creates a Loader with the given provider.
func NewLoader(p provider.Provider, opts ...LoaderOption) *Loader {
	l := &Loader{
		provider: p,
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Load reads, parses, and processes the configuration.
func (l *Loader) Load(ctx context.Context) (*Config, error) {
	// 1. Read raw bytes from provider
	data, err := l.provider.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// 2. Parse YAML/JSON into map
	rawMap, err := parseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// 3. Expand environment variables
	expandedMap := expandEnvVars(rawMap)

	// 4. Decode into Config struct
	cfg := &Config{}
	if err := decodeConfig(expandedMap, cfg); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	// 5. Apply defaults
	cfg.SetDefaults()

	// 6. Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Watch starts watching for config changes.
// When changes are detected, the config is reloaded and onChange is called.
// Blocks until ctx is cancelled.
func (l *Loader) Watch(ctx context.Context) error {
	changes, err := l.provider.Watch(ctx)
	if err != nil {
		return fmt.Errorf("failed to start watching: %w", err)
	}

	if changes == nil {
		slog.Info("Config watching not supported by provider", "type", l.provider.Type())
		<-ctx.Done()
		return ctx.Err()
	}

	slog.Info("Started watching for config changes", "type", l.provider.Type())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-changes:
			if !ok {
				return nil // Channel closed
			}

			cfg, err := l.Load(ctx)
			if err != nil {
				slog.Error("Failed to reload config", "error", err)
				continue
			}

			slog.Info("Configuration reloaded successfully")
			if l.onChange != nil {
				l.onChange(cfg)
			}
		}
	}
}

// Close releases resources held by the loader.
func (l *Loader) Close() error {
	return l.provider.Close()
}

// Provider returns the underlying provider (for hot-reload).
func (l *Loader) Provider() provider.Provider {
	return l.provider
}

// parseBytes parses raw bytes into a map.
// Supports YAML (primary) and JSON (fallback).
func parseBytes(data []byte) (map[string]any, error) {
	var result map[string]any

	// Try YAML first (YAML is a superset of JSON)
	if err := yaml.Unmarshal(data, &result); err == nil {
		return result, nil
	}

	// Fallback to JSON
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse as YAML or JSON: %w", err)
	}

	return result, nil
}

// stringToDurationHook is a DecodeHookFunc that converts strings to custom Duration type.
// Handles both "1s" format and integer nanoseconds.
func stringToDurationHook() mapstructure.DecodeHookFunc {
	return func(from, to reflect.Type, data any) (any, error) {
		// Check if target type is Duration (which is based on time.Duration)
		if to.Kind() == reflect.Int64 && to.Name() == "Duration" {
			// Handle string input (e.g., "1s", "30s")
			if from.Kind() == reflect.String {
				str := data.(string)
				parsed, err := time.ParseDuration(str)
				if err != nil {
					return nil, fmt.Errorf("invalid duration %q: %w", str, err)
				}
				return Duration(parsed), nil
			}

			// Handle integer input (nanoseconds)
			if from.Kind() == reflect.Int64 {
				return Duration(data.(int64)), nil
			}
			if from.Kind() == reflect.Int {
				return Duration(int64(data.(int))), nil
			}
		}

		return data, nil
	}
}

// decodeConfig decodes a map into a Config struct using mapstructure.
func decodeConfig(input map[string]any, output *Config) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           output,
		TagName:          "yaml",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			stringToDurationHook(),                      // Custom Duration type
			mapstructure.StringToTimeDurationHookFunc(), // Standard time.Duration
			mapstructure.StringToSliceHookFunc(","),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to create decoder: %w", err)
	}

	if err := decoder.Decode(input); err != nil {
		return fmt.Errorf("failed to decode: %w", err)
	}

	return nil
}

// expandEnvVars recursively expands ${VAR} and $VAR patterns in a map.
func expandEnvVars(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for k, v := range input {
		result[k] = expandValue(v)
	}
	return result
}

func expandValue(v any) any {
	switch val := v.(type) {
	case string:
		return expandEnvString(val)
	case map[string]any:
		return expandEnvVars(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = expandValue(item)
		}
		return result
	default:
		return v
	}
}

// envVarPattern matches ${VAR}, ${VAR:-default}, and $VAR
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

func expandEnvString(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		// Handle ${VAR} and ${VAR:-default}
		if strings.HasPrefix(match, "${") {
			inner := match[2 : len(match)-1] // Remove ${ and }

			// Check for default value syntax: ${VAR:-default}
			if idx := strings.Index(inner, ":-"); idx != -1 {
				varName := inner[:idx]
				defaultVal := inner[idx+2:]
				if val := os.Getenv(varName); val != "" {
					return val
				}
				return defaultVal
			}

			// Simple ${VAR}
			return os.Getenv(inner)
		}

		// Handle $VAR
		varName := match[1:] // Remove $
		return os.Getenv(varName)
	})
}

// LoadConfig is a convenience function that creates a loader and loads config.
func LoadConfig(ctx context.Context, opts provider.ProviderConfig) (*Config, *Loader, error) {
	p, err := provider.New(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create provider: %w", err)
	}

	loader := NewLoader(p)
	cfg, err := loader.Load(ctx)
	if err != nil {
		p.Close()
		return nil, nil, err
	}

	return cfg, loader, nil
}

// LoadConfigFile is a convenience function for loading from a file.
func LoadConfigFile(ctx context.Context, path string) (*Config, *Loader, error) {
	return LoadConfig(ctx, provider.ProviderConfig{
		Type: provider.TypeFile,
		Path: path,
	})
}
