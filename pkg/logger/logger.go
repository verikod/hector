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

package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
)

var defaultLogger *slog.Logger

const hectorPackagePrefix = "github.com/verikod/hector"

// ParseLevel converts a string log level to slog.Level
// Valid levels: debug, info, warn, error
func ParseLevel(levelStr string) (slog.Level, error) {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelWarn, nil
	}
}

// filteringHandler wraps a slog handler and filters third-party library logs
// Third-party logs are only shown when log level is DEBUG
type filteringHandler struct {
	handler  slog.Handler
	minLevel slog.Level
}

func (h *filteringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// First check if the log level itself is enabled
	if level < h.minLevel {
		return false
	}

	// If level is DEBUG, allow all logs (hector + third-party)
	if h.minLevel <= slog.LevelDebug {
		return h.handler.Enabled(ctx, level)
	}

	// For non-DEBUG levels, check if caller is from hector
	// We need to check the actual caller, so we'll do this in Handle()
	// For Enabled(), we'll be conservative and allow it, then filter in Handle()
	return h.handler.Enabled(ctx, level)
}

func (h *filteringHandler) Handle(ctx context.Context, record slog.Record) error {
	// If log level is DEBUG, allow all logs
	if h.minLevel <= slog.LevelDebug {
		return h.handler.Handle(ctx, record)
	}

	// For non-DEBUG levels, check if caller is from hector package
	if h.isHectorPackage(record.PC) {
		// Allow hector logs (respect log level)
		return h.handler.Handle(ctx, record)
	}

	// Filter out third-party logs when not in DEBUG mode
	return nil
}

func (h *filteringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &filteringHandler{
		handler:  h.handler.WithAttrs(attrs),
		minLevel: h.minLevel,
	}
}

func (h *filteringHandler) WithGroup(name string) slog.Handler {
	return &filteringHandler{
		handler:  h.handler.WithGroup(name),
		minLevel: h.minLevel,
	}
}

// isHectorPackage checks if the given PC (program counter) is from hector package
func (h *filteringHandler) isHectorPackage(pc uintptr) bool {
	if pc == 0 {
		return false
	}

	// Get function info from PC
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return false
	}

	// Get full function name (e.g., "github.com/verikod/hector/pkg/server/server.go")
	fullName := fn.Name()

	// Get file path
	file, _ := fn.FileLine(pc)

	// Check if it's from hector package
	// Check both function name and file path
	return strings.Contains(fullName, hectorPackagePrefix) ||
		strings.Contains(file, "hector/")
}

// getLevelColor returns ANSI color code for a log level
func getLevelColor(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "\033[31m" // Red for error
	case level >= slog.LevelWarn:
		return "\033[33m" // Yellow for warn
	case level >= slog.LevelInfo:
		return "\033[36m" // Cyan for info
	default:
		return "\033[90m" // Gray for debug
	}
}

// isTerminal checks if the file is a terminal
func isTerminal(file *os.File) bool {
	if fileInfo, err := file.Stat(); err == nil {
		return (fileInfo.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// coloredTextHandler wraps TextHandler and adds colors by formatting output directly
type coloredTextHandler struct {
	handler  slog.Handler
	writer   io.Writer
	useColor bool
	simple   bool // simple format: only level + message
}

func (h *coloredTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *coloredTextHandler) Handle(ctx context.Context, record slog.Record) error {
	if !h.useColor {
		return h.handler.Handle(ctx, record)
	}

	// Format with colors
	colorCode := getLevelColor(record.Level)
	resetCode := "\033[0m"

	var buf strings.Builder

	// Simple format: level + message + attributes
	if h.simple {
		levelStr := record.Level.String()
		if levelStr == "WARNING" {
			levelStr = "WARN"
		}
		buf.WriteString(colorCode)
		buf.WriteString(strings.ToUpper(levelStr))
		buf.WriteString(resetCode)
		buf.WriteString(" ")
		buf.WriteString(record.Message)

		// Include attributes in simple format
		record.Attrs(func(a slog.Attr) bool {
			buf.WriteString(" ")
			buf.WriteString(a.Key)
			buf.WriteString("=")
			buf.WriteString(a.Value.String())
			return true
		})

		buf.WriteString("\n")
	} else {
		// Verbose format: time + level + message + attributes
		if !record.Time.IsZero() {
			buf.WriteString(record.Time.Format("2006/01/02 15:04:05 "))
		}

		levelStr := record.Level.String()
		if levelStr == "WARNING" {
			levelStr = "WARN"
		}
		buf.WriteString(colorCode)
		buf.WriteString(strings.ToUpper(levelStr))
		buf.WriteString(resetCode)
		buf.WriteString(" ")
		buf.WriteString(record.Message)

		// Attributes
		record.Attrs(func(a slog.Attr) bool {
			buf.WriteString(" ")
			buf.WriteString(a.Key)
			buf.WriteString("=")
			buf.WriteString(a.Value.String())
			return true
		})

		buf.WriteString("\n")
	}

	_, err := h.writer.Write([]byte(buf.String()))
	return err
}

func (h *coloredTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &coloredTextHandler{
		handler:  h.handler.WithAttrs(attrs),
		writer:   h.writer,
		useColor: h.useColor,
		simple:   h.simple,
	}
}

func (h *coloredTextHandler) WithGroup(name string) slog.Handler {
	return &coloredTextHandler{
		handler:  h.handler.WithGroup(name),
		writer:   h.writer,
		useColor: h.useColor,
		simple:   h.simple,
	}
}

// Init initializes the logger with the specified level and format
// Third-party library logs are only shown when level is DEBUG
// Color support is enabled automatically for terminal output
// format: "simple" (level + message only), "verbose" (time + level + message + attributes),
//
//	or any custom value (falls back to default slog.TextHandler format)
func Init(level slog.Level, output *os.File, format string) {
	useColor := isTerminal(output)
	simple := format == "simple" || format == "" // default to simple
	verbose := format == "verbose"

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Normalize WARNING to WARN
			if a.Key == slog.LevelKey {
				levelStr := a.Value.String()
				if levelStr == "WARNING" {
					return slog.String("level", "WARN")
				}
			}
			return a
		},
	}

	baseHandler := slog.NewTextHandler(output, opts)

	// Wrap with colored handler if terminal
	var handler slog.Handler = baseHandler
	if useColor {
		// For terminal output with custom formats, use colored handler
		if simple || verbose {
			handler = &coloredTextHandler{
				handler:  baseHandler,
				writer:   output,
				useColor: true,
				simple:   simple,
			}
		}
		// For custom formats in terminal, baseHandler will be used (standard slog format with colors via ReplaceAttr)
	} else if simple {
		// For non-terminal simple format, create a custom handler
		handler = &simpleTextHandler{
			handler: baseHandler,
			writer:  output,
		}
	}
	// For verbose or custom formats in non-terminal, use baseHandler (standard slog format)

	// Wrap with filtering handler
	filteringHandler := &filteringHandler{
		handler:  handler,
		minLevel: level,
	}

	defaultLogger = slog.New(filteringHandler)

	// Set as default logger - all libraries using slog will use this
	slog.SetDefault(defaultLogger)
}

// simpleTextHandler formats logs in simple format (level + message only) for non-terminal output
type simpleTextHandler struct {
	handler slog.Handler
	writer  io.Writer
}

func (h *simpleTextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *simpleTextHandler) Handle(ctx context.Context, record slog.Record) error {
	var buf strings.Builder

	levelStr := record.Level.String()
	if levelStr == "WARNING" {
		levelStr = "WARN"
	}
	buf.WriteString(strings.ToUpper(levelStr))
	buf.WriteString(" ")
	buf.WriteString(record.Message)

	// Include attributes in simple format
	record.Attrs(func(a slog.Attr) bool {
		buf.WriteString(" ")
		buf.WriteString(a.Key)
		buf.WriteString("=")
		buf.WriteString(a.Value.String())
		return true
	})

	buf.WriteString("\n")

	_, err := h.writer.Write([]byte(buf.String()))
	return err
}

func (h *simpleTextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &simpleTextHandler{
		handler: h.handler.WithAttrs(attrs),
		writer:  h.writer,
	}
}

func (h *simpleTextHandler) WithGroup(name string) slog.Handler {
	return &simpleTextHandler{
		handler: h.handler.WithGroup(name),
		writer:  h.writer,
	}
}

// OpenLogFile opens or creates a log file at the specified path
// Returns the file handle and a cleanup function, or an error
func OpenLogFile(path string) (*os.File, func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		file.Close()
	}

	return file, cleanup, nil
}

// GetLogger returns the default slog logger
func GetLogger() *slog.Logger {
	if defaultLogger == nil {
		// Initialize with default level and format if not already done (INFO level, simple format)
		Init(slog.LevelInfo, os.Stderr, "simple")
	}
	return defaultLogger
}
