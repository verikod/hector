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

package ratelimit

import (
	"fmt"

	"github.com/verikod/hector/pkg/config"
)

// NewRateLimiterFromConfig creates a RateLimiter from configuration.
// Uses v2's database configuration foundation (DBPool and DatabaseConfig).
// If rate limiting is disabled, returns nil.
//
// Example config:
//
//	databases:
//	  default:
//	    driver: sqlite
//	    database: ./.hector/hector.db
//
//	rate_limiting:
//	  enabled: true
//	  backend: sql
//	  sql_database: default
//	  limits:
//	    - type: token
//	      window: day
//	      limit: 100000
func NewRateLimiterFromConfig(cfg *config.Config, pool *config.DBPool) (RateLimiter, error) {
	rateLimitCfg := cfg.RateLimiting
	if rateLimitCfg == nil || !rateLimitCfg.IsEnabled() {
		return nil, nil
	}

	// Create store based on backend
	var store Store

	switch rateLimitCfg.Backend {
	case "sql":
		// DBPool is required for SQL backends
		if pool == nil {
			return nil, fmt.Errorf("DBPool is required for SQL rate limit backend")
		}

		// Get database reference
		dbName := rateLimitCfg.SQLDatabase
		if dbName == "" {
			return nil, fmt.Errorf("rate_limiting.sql_database is required when backend is sql")
		}

		dbCfg, ok := cfg.GetDatabase(dbName)
		if !ok {
			return nil, fmt.Errorf("database %q not found", dbName)
		}

		// Get connection from pool (shares connection with other components)
		db, err := pool.Get(dbCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to get database connection: %w", err)
		}

		store, err = NewSQLStore(db, dbCfg.Dialect())
		if err != nil {
			return nil, fmt.Errorf("failed to create SQL store: %w", err)
		}
	case "memory", "":
		store = NewMemoryStore()
	default:
		return nil, fmt.Errorf("unsupported rate limit backend: %s", rateLimitCfg.Backend)
	}

	// Convert config limits to LimitRules
	limits := make([]LimitRule, len(rateLimitCfg.Limits))
	for i, l := range rateLimitCfg.Limits {
		limits[i] = LimitRule{
			Type:   ParseLimitType(l.Type),
			Window: ParseTimeWindow(l.Window),
			Limit:  l.Limit,
		}
	}

	// Create limiter config
	limiterCfg := &Config{
		Enabled: rateLimitCfg.IsEnabled(),
		Limits:  limits,
	}

	return NewRateLimiter(limiterCfg, store)
}

// NewRateLimiterFromConfigWithStore creates a RateLimiter with a custom store.
// Useful for testing or when you need to share a store across multiple limiters.
func NewRateLimiterFromConfigWithStore(cfg *config.RateLimitConfig, store Store) (RateLimiter, error) {
	if cfg == nil || !cfg.IsEnabled() {
		return nil, nil
	}

	if store == nil {
		return nil, fmt.Errorf("store is required")
	}

	// Convert config limits to LimitRules
	limits := make([]LimitRule, len(cfg.Limits))
	for i, l := range cfg.Limits {
		limits[i] = LimitRule{
			Type:   ParseLimitType(l.Type),
			Window: ParseTimeWindow(l.Window),
			Limit:  l.Limit,
		}
	}

	// Create limiter config
	limiterCfg := &Config{
		Enabled: cfg.IsEnabled(),
		Limits:  limits,
	}

	return NewRateLimiter(limiterCfg, store)
}

// ScopeFromConfig returns the rate limiting scope from configuration.
func ScopeFromConfig(cfg *config.RateLimitConfig) Scope {
	if cfg == nil || cfg.Scope == "" {
		return ScopeSession
	}
	return ParseScope(cfg.Scope)
}
