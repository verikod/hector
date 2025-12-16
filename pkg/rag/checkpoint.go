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

package rag

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/verikod/hector/pkg/utils"
)

// IndexCheckpoint represents a saved indexing checkpoint.
//
// Direct port from legacy pkg/context/checkpoint.go
type IndexCheckpoint struct {
	Version        string                    `json:"version"`
	StoreName      string                    `json:"store_name"`
	SourcePath     string                    `json:"source_path"`
	StartTime      time.Time                 `json:"start_time"`
	LastUpdate     time.Time                 `json:"last_update"`
	ProcessedFiles map[string]FileCheckpoint `json:"processed_files"`
	TotalFiles     int                       `json:"total_files"`
	IndexedCount   int                       `json:"indexed_count"`
	SkippedCount   int                       `json:"skipped_count"`
	FailedCount    int                       `json:"failed_count"`
}

// FileCheckpoint contains information about a processed file.
//
// Direct port from legacy pkg/context/checkpoint.go
type FileCheckpoint struct {
	Path        string    `json:"path"`
	Hash        string    `json:"hash"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	Status      string    `json:"status"` // "indexed", "skipped", "failed"
	ProcessedAt time.Time `json:"processed_at"`
}

// IndexCheckpointManager manages indexing checkpoints.
//
// Direct port from legacy pkg/context/checkpoint.go
type IndexCheckpointManager struct {
	checkpointDir string
	sourcePath    string // Source path being indexed
	storeName     string
	checkpoint    *IndexCheckpoint
	enabled       bool
	saveInterval  time.Duration
	lastSaveTime  time.Time
	mu            sync.RWMutex // Protects checkpoint data
}

// NewIndexCheckpointManager creates a new checkpoint manager.
//
// Direct port from legacy pkg/context/checkpoint.go
func NewIndexCheckpointManager(storeName, sourcePath string, enabled bool) *IndexCheckpointManager {
	// Store checkpoints in .hector/checkpoints/ within the source directory
	// This keeps checkpoints co-located with the data and allows easy cleanup
	hectorDir, _ := utils.EnsureHectorDir(sourcePath) // Ignore error - will fail on first save if needed
	checkpointDir := filepath.Join(hectorDir, "checkpoints")
	_ = os.MkdirAll(checkpointDir, 0755) // Ensure checkpoints subdirectory exists

	return &IndexCheckpointManager{
		checkpointDir: checkpointDir,
		sourcePath:    sourcePath,
		storeName:     storeName,
		enabled:       enabled,
		saveInterval:  10 * time.Second, // Save every 10 seconds
		checkpoint: &IndexCheckpoint{
			Version:        "1.0",
			StoreName:      storeName,
			SourcePath:     sourcePath,
			StartTime:      time.Now(),
			LastUpdate:     time.Now(),
			ProcessedFiles: make(map[string]FileCheckpoint),
		},
	}
}

// LoadCheckpoint attempts to load an existing checkpoint.
func (cm *IndexCheckpointManager) LoadCheckpoint() (*IndexCheckpoint, error) {
	if !cm.enabled {
		return nil, nil
	}

	checkpointPath := cm.getCheckpointPath()
	data, err := os.ReadFile(checkpointPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No checkpoint exists
		}
		return nil, fmt.Errorf("failed to read checkpoint: %w", err)
	}

	var checkpoint IndexCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, fmt.Errorf("failed to parse checkpoint: %w", err)
	}

	cm.checkpoint = &checkpoint
	return &checkpoint, nil
}

// SaveCheckpoint saves the current checkpoint.
func (cm *IndexCheckpointManager) SaveCheckpoint() error {
	if !cm.enabled {
		return nil
	}

	cm.mu.Lock()
	// Throttle saves to avoid excessive I/O
	if time.Since(cm.lastSaveTime) < cm.saveInterval {
		cm.mu.Unlock()
		return nil
	}

	// Create a snapshot to minimize lock hold time
	checkpointCopy := IndexCheckpoint{
		Version:        cm.checkpoint.Version,
		StoreName:      cm.checkpoint.StoreName,
		SourcePath:     cm.checkpoint.SourcePath,
		StartTime:      cm.checkpoint.StartTime,
		LastUpdate:     time.Now(),
		TotalFiles:     cm.checkpoint.TotalFiles,
		IndexedCount:   cm.checkpoint.IndexedCount,
		SkippedCount:   cm.checkpoint.SkippedCount,
		FailedCount:    cm.checkpoint.FailedCount,
		ProcessedFiles: make(map[string]FileCheckpoint, len(cm.checkpoint.ProcessedFiles)),
	}

	// Deep copy the processed files map
	for k, v := range cm.checkpoint.ProcessedFiles {
		checkpointCopy.ProcessedFiles[k] = v
	}

	cm.lastSaveTime = time.Now()
	cm.mu.Unlock()

	// Marshal without holding lock
	data, err := json.MarshalIndent(&checkpointCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checkpoint: %w", err)
	}

	checkpointPath := cm.getCheckpointPath()
	if err := os.WriteFile(checkpointPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write checkpoint: %w", err)
	}

	return nil
}

// ForceSave forces a checkpoint save regardless of the save interval.
func (cm *IndexCheckpointManager) ForceSave() error {
	if !cm.enabled {
		return nil
	}

	cm.mu.Lock()
	cm.lastSaveTime = time.Time{} // Reset to force save
	cm.mu.Unlock()

	return cm.SaveCheckpoint()
}

// RecordFile records a processed file in the checkpoint.
func (cm *IndexCheckpointManager) RecordFile(path string, size int64, modTime time.Time, status string) {
	if !cm.enabled {
		return
	}

	hash := cm.computeFileHash(path, size, modTime)

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.checkpoint.ProcessedFiles[path] = FileCheckpoint{
		Path:        path,
		Hash:        hash,
		Size:        size,
		ModTime:     modTime,
		Status:      status,
		ProcessedAt: time.Now(),
	}

	// Update counters
	switch status {
	case "indexed":
		cm.checkpoint.IndexedCount++
	case "skipped":
		cm.checkpoint.SkippedCount++
	case "failed":
		cm.checkpoint.FailedCount++
	}
}

// ShouldProcessFile checks if a file should be processed (not in checkpoint or changed).
func (cm *IndexCheckpointManager) ShouldProcessFile(path string, size int64, modTime time.Time) bool {
	if !cm.enabled {
		return true
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	fileCheckpoint, exists := cm.checkpoint.ProcessedFiles[path]
	if !exists {
		return true // File not in checkpoint
	}

	// Check if file has changed
	currentHash := cm.computeFileHash(path, size, modTime)
	if currentHash != fileCheckpoint.Hash {
		return true // File has changed
	}

	// Skip if previously indexed or skipped successfully
	return fileCheckpoint.Status == "failed"
}

// SetTotalFiles sets the total file count.
func (cm *IndexCheckpointManager) SetTotalFiles(total int) {
	if cm.enabled {
		cm.mu.Lock()
		cm.checkpoint.TotalFiles = total
		cm.mu.Unlock()
	}
}

// GetProcessedCount returns the number of processed files.
func (cm *IndexCheckpointManager) GetProcessedCount() int {
	if !cm.enabled {
		return 0
	}
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return len(cm.checkpoint.ProcessedFiles)
}

// ClearCheckpoint removes the checkpoint file.
func (cm *IndexCheckpointManager) ClearCheckpoint() error {
	if !cm.enabled {
		return nil
	}

	checkpointPath := cm.getCheckpointPath()
	if err := os.Remove(checkpointPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove checkpoint: %w", err)
	}

	return nil
}

// IsEnabled returns whether checkpointing is enabled.
func (cm *IndexCheckpointManager) IsEnabled() bool {
	return cm.enabled
}

// getCheckpointPath returns the path to the checkpoint file.
func (cm *IndexCheckpointManager) getCheckpointPath() string {
	// Use store name directly for the filename since checkpoints are already
	// in a directory specific to the source path (.hector/checkpoints/)
	filename := fmt.Sprintf("checkpoint_%s.json", cm.storeName)
	return filepath.Join(cm.checkpointDir, filename)
}

// computeFileHash computes a hash for file identification.
func (cm *IndexCheckpointManager) computeFileHash(path string, size int64, modTime time.Time) string {
	data := fmt.Sprintf("%s:%d:%d", path, size, modTime.Unix())
	hash := md5.Sum([]byte(data))
	return fmt.Sprintf("%x", hash)
}

// FormatCheckpointInfo returns a human-readable checkpoint summary.
func (cm *IndexCheckpointManager) FormatCheckpointInfo(checkpoint *IndexCheckpoint) string {
	if checkpoint == nil {
		return ""
	}

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	processed := len(checkpoint.ProcessedFiles)
	elapsed := time.Since(checkpoint.StartTime)

	return fmt.Sprintf("Found checkpoint: %d/%d files processed (%d indexed, %d skipped, %d failed) - %s elapsed",
		processed, checkpoint.TotalFiles,
		checkpoint.IndexedCount, checkpoint.SkippedCount, checkpoint.FailedCount,
		formatDurationForCheckpoint(elapsed))
}

// formatDurationForCheckpoint formats a duration in a human-readable way.
func formatDurationForCheckpoint(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", mins, secs)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", hours, mins)
}
