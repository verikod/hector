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

package session

import (
	"fmt"

	"github.com/verikod/hector/pkg/config"
)

// NewSessionServiceFromConfig creates a session Service based on configuration.
// DBPool is required for SQL backends to share connections and prevent lock errors.
// Returns InMemoryService if no session persistence is configured.
//
// Example config:
//
//	databases:
//	  default:
//	    driver: sqlite
//	    database: ./.hector/hector.db
//
//	server:
//	  sessions:
//	    backend: sql
//	    database: default
func NewSessionServiceFromConfig(cfg *config.Config, pool *config.DBPool) (Service, error) {
	// Check if sessions config exists and is SQL
	if cfg.Storage.Sessions == nil || cfg.Storage.Sessions.IsInMemory() {
		// Return in-memory service (default)
		return InMemoryService(), nil
	}

	if !cfg.Storage.Sessions.IsSQL() {
		return nil, fmt.Errorf("unknown sessions backend: %s", cfg.Storage.Sessions.Backend)
	}

	// DBPool is required for SQL backends
	if pool == nil {
		return nil, fmt.Errorf("DBPool is required for SQL session backend")
	}

	// Get database reference
	dbName := cfg.Storage.Sessions.Database
	dbCfg, ok := cfg.GetDatabase(dbName)
	if !ok {
		return nil, fmt.Errorf("database %q not found", dbName)
	}

	db, err := pool.Get(dbCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get database connection: %w", err)
	}
	return NewSQLSessionService(db, dbCfg.Dialect())
}
