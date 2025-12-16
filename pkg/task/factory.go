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

package task

import (
	"fmt"

	"github.com/a2aproject/a2a-go/a2asrv"

	"github.com/verikod/hector/pkg/config"
)

// NewTaskStoreFromConfig creates a TaskStore based on configuration.
// DBPool is required for SQL backends to share connections and prevent lock errors.
// Returns nil if no task persistence is configured (a2a-go uses in-memory).
//
// Example config:
//
//	databases:
//	  default:
//	    driver: sqlite
//	    database: ./.hector/tasks.db
//
//	server:
//	  tasks:
//	    backend: sql
//	    database: default
func NewTaskStoreFromConfig(cfg *config.Config, pool *config.DBPool) (a2asrv.TaskStore, error) {
	// Check if tasks config exists and is SQL
	if cfg.Server.Tasks == nil || cfg.Server.Tasks.IsInMemory() {
		// Return nil - a2a-go will use its internal in-memory store
		return nil, nil
	}

	if !cfg.Server.Tasks.IsSQL() {
		return nil, fmt.Errorf("unknown tasks backend: %s", cfg.Server.Tasks.Backend)
	}

	// DBPool is required for SQL backends
	if pool == nil {
		return nil, fmt.Errorf("DBPool is required for SQL task backend")
	}

	// Get database reference
	dbName := cfg.Server.Tasks.Database
	dbCfg, ok := cfg.GetDatabase(dbName)
	if !ok {
		return nil, fmt.Errorf("database %q not found", dbName)
	}

	db, err := pool.Get(dbCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	return NewSQLTaskStore(db, dbCfg.Dialect())
}
