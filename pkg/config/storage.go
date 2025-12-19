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

// StorageConfig configures data storage and persistence.
//
// This section is separate from ServerConfig for security reasons:
// - Storage settings CAN be modified via Studio API
// - Server settings (auth, TLS, ports) CANNOT be modified remotely
//
// Example:
//
//	storage:
//	  tasks:
//	    backend: sql
//	    database: main
//	  sessions:
//	    backend: sql
//	    database: main
//	  memory:
//	    backend: vector
//	    embedder: default
//	  checkpoint:
//	    enabled: true
//	    strategy: hybrid
type StorageConfig struct {
	// Tasks configures the task store for A2A task persistence.
	Tasks *TasksConfig `yaml:"tasks,omitempty" json:"tasks,omitempty" jsonschema:"title=Tasks,description=Task store configuration"`

	// Sessions configures the session store for conversation persistence.
	Sessions *SessionsConfig `yaml:"sessions,omitempty" json:"sessions,omitempty" jsonschema:"title=Sessions,description=Session store configuration"`

	// Memory configures the memory service for cross-session knowledge.
	Memory *MemoryConfig `yaml:"memory,omitempty" json:"memory,omitempty" jsonschema:"title=Memory,description=Memory index configuration"`

	// Checkpoint configures execution state checkpointing and recovery.
	Checkpoint *CheckpointConfig `yaml:"checkpoint,omitempty" json:"checkpoint,omitempty" jsonschema:"title=Checkpoint,description=Checkpoint and recovery configuration"`
}

// SetDefaults applies default values for StorageConfig.
func (c *StorageConfig) SetDefaults() {
	if c.Tasks != nil {
		c.Tasks.SetDefaults()
	}
	if c.Sessions != nil {
		c.Sessions.SetDefaults()
	}
	if c.Memory != nil {
		c.Memory.SetDefaults()
	}
	if c.Checkpoint != nil {
		c.Checkpoint.SetDefaults()
	}
}

// Validate checks the storage configuration.
func (c *StorageConfig) Validate() error {
	if c.Tasks != nil {
		if err := c.Tasks.Validate(); err != nil {
			return err
		}
	}
	if c.Sessions != nil {
		if err := c.Sessions.Validate(); err != nil {
			return err
		}
	}
	if c.Memory != nil {
		if err := c.Memory.Validate(); err != nil {
			return err
		}
	}
	if c.Checkpoint != nil {
		if err := c.Checkpoint.Validate(); err != nil {
			return err
		}
	}
	return nil
}
