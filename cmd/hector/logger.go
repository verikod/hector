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
	"fmt"
	"os"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/logger"
)

const (
	// DefaultLogFile is the default log file name for client/local modes
	// Can be overridden via LOG_FILE environment variable
	DefaultLogFile = "hector.log"
	// LogFileEnvVar is the environment variable name for log file path
	LogFileEnvVar = "LOG_FILE"
	// LogLevelEnvVar is the environment variable name for log level
	LogLevelEnvVar = "LOG_LEVEL"
	// LogFormatEnvVar is the environment variable name for log format
	LogFormatEnvVar = "LOG_FORMAT"
	// DefaultLogFormat is the default log format
	DefaultLogFormat = "simple"
)

// initLoggerFromCLI initializes the logger from CLI flags and environment variables.
// Priority: CLI flags > env vars > defaults
// Returns: level string, file string, format string, cleanup function, error
func initLoggerFromCLI(cliLogLevel, cliLogFile, cliLogFormat string) (string, string, string, func(), error) {
	// Determine log level: CLI flag > env var > default
	logLevel := cliLogLevel
	if logLevel == "" {
		logLevel = os.Getenv(LogLevelEnvVar)
	}
	if logLevel == "" {
		logLevel = "info" // default
	}

	// Determine log file: CLI flag > env var > default (empty = stderr)
	logFile := cliLogFile
	if logFile == "" {
		logFile = os.Getenv(LogFileEnvVar)
	}

	// Determine log format: CLI flag > env var > default
	logFormat := cliLogFormat
	if logFormat == "" {
		logFormat = os.Getenv(LogFormatEnvVar)
	}
	if logFormat == "" {
		logFormat = DefaultLogFormat
	}

	// Parse level
	level, err := logger.ParseLevel(logLevel)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Determine output file
	var output *os.File
	var cleanup func()
	if logFile != "" {
		file, cleanupFn, err := logger.OpenLogFile(logFile)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("failed to open log file: %w", err)
		}
		output = file
		cleanup = cleanupFn
	} else {
		output = os.Stderr
		cleanup = nil
	}

	// Initialize logger
	logger.Init(level, output, logFormat)

	return logLevel, logFile, logFormat, cleanup, nil
}

// initLoggerFromConfig initializes the logger from config file settings.
// This is called after config loading if CLI/env didn't override.
// Returns: level string, file string, format string, cleanup function, error
//
//nolint:unused // Reserved for future use
func initLoggerFromConfig(cfg *config.LoggerConfig) (string, string, string, func(), error) {
	if cfg == nil {
		return "", "", "", nil, nil
	}

	// Use config file values (already have defaults applied)
	logLevel := cfg.Level
	if logLevel == "" {
		logLevel = "info"
	}

	logFile := cfg.File
	logFormat := cfg.Format
	if logFormat == "" {
		logFormat = DefaultLogFormat
	}

	// Parse level
	level, err := logger.ParseLevel(logLevel)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Determine output file
	var output *os.File
	var cleanup func()
	if logFile != "" {
		file, cleanupFn, err := logger.OpenLogFile(logFile)
		if err != nil {
			return "", "", "", nil, fmt.Errorf("failed to open log file: %w", err)
		}
		output = file
		cleanup = cleanupFn
	} else {
		output = os.Stderr
		cleanup = nil
	}

	// Initialize logger
	logger.Init(level, output, logFormat)

	return logLevel, logFile, logFormat, cleanup, nil
}

// determineLogFormat determines the log format based on priority: CLI flag > env var > default
//
//nolint:unused // Reserved for future use
func determineLogFormat(cliLogFormat string) string {
	// Priority: CLI flag > env var > default
	if cliLogFormat != "" {
		return cliLogFormat
	}
	if envLogFormat := os.Getenv(LogFormatEnvVar); envLogFormat != "" {
		return envLogFormat
	}
	return DefaultLogFormat
}
