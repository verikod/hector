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

package provider

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileProvider loads config from a local file and watches for changes.
type FileProvider struct {
	path string

	mu      sync.Mutex
	watcher *fsnotify.Watcher
	closed  bool
}

// NewFileProvider creates a provider that reads from a local file.
func NewFileProvider(path string) (*FileProvider, error) {
	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	return &FileProvider{
		path: absPath,
	}, nil
}

// Type returns TypeFile.
func (p *FileProvider) Type() Type {
	return TypeFile
}

// Load reads the config file.
func (p *FileProvider) Load(ctx context.Context) ([]byte, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", p.path, err)
	}
	return data, nil
}

// Path returns the path to the config file.
func (p *FileProvider) Path() string {
	return p.path
}

// Watch starts watching the config file for changes.
// Returns a channel that receives a value when the file changes.
func (p *FileProvider) Watch(ctx context.Context) (<-chan struct{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("provider is closed")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}
	p.watcher = watcher

	// Watch the directory containing the file
	// (some systems don't support watching files directly)
	configDir := filepath.Dir(p.path)
	configFile := filepath.Base(p.path)

	if err := watcher.Add(configDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch directory %s: %w", configDir, err)
	}

	ch := make(chan struct{}, 1) // Buffered to avoid blocking

	go p.watchLoop(ctx, watcher, configFile, ch)

	slog.Info("Watching config file", "path", p.path)
	return ch, nil
}

func (p *FileProvider) watchLoop(ctx context.Context, watcher *fsnotify.Watcher, configFile string, ch chan<- struct{}) {
	defer close(ch)
	defer watcher.Close()

	// Debounce timer to coalesce rapid changes
	var debounceTimer *time.Timer
	const debounceDelay = 100 * time.Millisecond

	// Also watch for .env changes
	const envFile = ".env"

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// React to changes in config file OR .env file
			eventFile := filepath.Base(event.Name)
			isConfigFile := eventFile == configFile
			isEnvFile := eventFile == envFile

			if !isConfigFile && !isEnvFile {
				continue
			}

			if event.Op&fsnotify.Write == fsnotify.Write ||
				event.Op&fsnotify.Create == fsnotify.Create {
				// Debounce: reset timer on each change
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					select {
					case ch <- struct{}{}:
						if isEnvFile {
							slog.Debug(".env file changed", "path", filepath.Join(filepath.Dir(p.path), envFile))
						} else {
							slog.Debug("Config file changed", "path", p.path)
						}
					default:
						// Channel full, change already pending
					}
				})
			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				if isConfigFile {
					slog.Warn("Config file was deleted", "path", p.path)
					// Try to re-add watch if file is recreated
					go p.tryRewatch(ctx, watcher, configFile, ch)
				}
				// For .env deletion, we don't try to rewatch - it's optional
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Error("File watcher error", "error", err)
		}
	}
}

func (p *FileProvider) tryRewatch(ctx context.Context, watcher *fsnotify.Watcher, configFile string, ch chan<- struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for i := 0; i < 10; i++ { // Try for 5 seconds
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(p.path); err == nil {
				configDir := filepath.Dir(p.path)
				if err := watcher.Add(configDir); err == nil {
					slog.Info("Re-established watch on config file", "path", p.path)
					// Signal change since file was recreated
					select {
					case ch <- struct{}{}:
					default:
					}
					return
				}
			}
		}
	}
	slog.Warn("Failed to re-establish watch on config file", "path", p.path)
}

// Close stops watching and releases resources.
func (p *FileProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	if p.watcher != nil {
		err := p.watcher.Close()
		p.watcher = nil
		return err
	}
	return nil
}

// Ensure FileProvider implements Provider
var _ Provider = (*FileProvider)(nil)
