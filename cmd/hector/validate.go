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
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/verikod/hector/pkg/config"
	"gopkg.in/yaml.v3"
)

// ValidateCmd validates a configuration file.
// Ported from legacy pkg/cli/validate_command.go
type ValidateCmd struct {
	// Config is the configuration file path (positional argument)
	Config string `arg:"" name:"config" help:"Configuration file path." placeholder:"PATH"`

	// Format specifies the output format
	Format string `short:"f" help:"Output format: compact, verbose, json." default:"compact" enum:"compact,verbose,json"`

	// PrintConfig prints the expanded configuration
	PrintConfig bool `short:"p" name:"print-config" help:"Print the expanded configuration (with defaults applied and env vars resolved)."`
}

// Run executes the validate command.
// Line-by-line port from legacy ValidateCommand function.
func (c *ValidateCmd) Run(cli *CLI) error {
	ctx := context.Background()

	// Load .env file if it exists next to the config file
	// pkg adaptation: Use config.LoadDotEnvForConfig
	_ = config.LoadDotEnvForConfig(c.Config)

	// Load configuration using pkg's config loader
	// Legacy used config.LoadConfig with LoaderOptions
	// pkg adaptation: Use config.LoadConfigFile which handles loading and validation
	cfg, loader, err := config.LoadConfigFile(ctx, c.Config)
	if err != nil {
		return printLoadError(c.Format, c.Config, err)
	}
	if loader != nil {
		defer loader.Close()
	}

	// pkg note: config.LoadConfigFile already calls SetDefaults() and Validate()
	// Legacy used ProcessConfigPipeline for this, but pkg's loader handles it internally
	// This means validation is complete at this point

	// If --print-config is specified, print the expanded configuration
	if c.PrintConfig {
		return printExpandedConfig(c.Format, c.Config, cfg)
	}

	// Success - configuration is valid
	printSuccess(c.Format, c.Config)
	return nil
}

// ValidationError represents a single validation error.
// Ported from legacy pkg/cli/validate_command.go
type ValidationError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// printLoadError prints a configuration load error.
// Ported line-by-line from legacy.
func printLoadError(format, file string, err error) error {
	switch format {
	case "json":
		printJSONResult(false, file, []ValidationError{{Type: "load", Message: err.Error()}})
	case "verbose":
		fmt.Fprintf(os.Stderr, "Configuration Load Error\n")
		fmt.Fprintf(os.Stderr, "========================\n\n")
		fmt.Fprintf(os.Stderr, "File:    %s\n", file)
		fmt.Fprintf(os.Stderr, "Error:   %s\n", err.Error())
	default: // compact
		fmt.Fprintf(os.Stderr, "%s: load error: %s\n", file, err.Error())
	}
	return fmt.Errorf("config load failed")
}

// printProcessError prints a configuration processing error.
// Ported line-by-line from legacy.
// Note: In pkg, processing errors are typically caught during LoadConfigFile,
// but this function is kept for consistency and potential future use.
//
//nolint:unused // Reserved for future use
func printProcessError(format, file string, err error) error {
	switch format {
	case "json":
		printJSONResult(false, file, []ValidationError{{Type: "process", Message: err.Error()}})
	case "verbose":
		fmt.Fprintf(os.Stderr, "Configuration Processing Error\n")
		fmt.Fprintf(os.Stderr, "==============================\n\n")
		fmt.Fprintf(os.Stderr, "File:    %s\n", file)
		fmt.Fprintf(os.Stderr, "Error:   %s\n", err.Error())
	default: // compact
		fmt.Fprintf(os.Stderr, "%s: process error: %s\n", file, err.Error())
	}
	return fmt.Errorf("config processing failed")
}

// printSuccess prints a success message.
// Ported line-by-line from legacy.
func printSuccess(format, file string) {
	switch format {
	case "json":
		printJSONResult(true, file, nil)
	case "verbose":
		fmt.Fprintf(os.Stdout, "Configuration Validation Successful\n")
		fmt.Fprintf(os.Stdout, "===================================\n\n")
		fmt.Fprintf(os.Stdout, "File:   %s\n", file)
		fmt.Fprintf(os.Stdout, "Status: OK Valid\n")
	default: // compact
		fmt.Fprintf(os.Stdout, "%s: valid\n", file)
	}
}

// printExpandedConfig prints the expanded configuration.
// Ported line-by-line from legacy.
func printExpandedConfig(format, file string, cfg *config.Config) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(cfg); err != nil {
			return fmt.Errorf("failed to encode config as JSON: %w", err)
		}
	case "verbose", "compact":
		// Use YAML for human-readable output (both verbose and compact use same format)
		fmt.Fprintf(os.Stdout, "# Expanded Configuration from: %s\n", file)
		fmt.Fprintf(os.Stdout, "# (defaults applied, env vars resolved)\n\n")

		encoder := yaml.NewEncoder(os.Stdout)
		encoder.SetIndent(2)
		if err := encoder.Encode(cfg); err != nil {
			return fmt.Errorf("failed to encode config as YAML: %w", err)
		}
		encoder.Close()
	}
	return nil
}

// jsonOutput is the JSON output structure.
// Ported from legacy.
type jsonOutput struct {
	Valid  bool              `json:"valid"`
	File   string            `json:"file"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// printJSONResult prints a JSON validation result.
// Ported line-by-line from legacy.
func printJSONResult(valid bool, file string, errors []ValidationError) {
	output := jsonOutput{
		Valid:  valid,
		File:   file,
		Errors: errors,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
	}
}
