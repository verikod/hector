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
	"fmt"
	"time"
)

// AuthConfig configures JWT-based authentication for the server.
//
// Authentication is disabled by default. When enabled, all endpoints
// (except health checks and agent discovery) require a valid JWT token.
//
// Example configuration:
//
//	server:
//	  auth:
//	    enabled: true
//	    jwks_url: "https://auth.example.com/.well-known/jwks.json"
//	    issuer: "https://auth.example.com"
//	    audience: "hector-api"
//
// The JWT token should be passed in the Authorization header:
//
//	Authorization: Bearer <token>
type AuthConfig struct {
	// Enabled controls whether authentication is required.
	// Default: false
	Enabled bool `yaml:"enabled,omitempty"`

	// JWKSURL is the URL to fetch JSON Web Key Set from.
	// Required when Enabled is true.
	// Example: "https://auth.example.com/.well-known/jwks.json"
	JWKSURL string `yaml:"jwks_url,omitempty"`

	// Issuer is the expected token issuer (iss claim).
	// Required when Enabled is true.
	// Example: "https://auth.example.com"
	Issuer string `yaml:"issuer,omitempty"`

	// Audience is the expected token audience (aud claim).
	// Required when Enabled is true.
	// Example: "hector-api"
	Audience string `yaml:"audience,omitempty"`

	// ClientID is the public Client ID for the frontend app.
	// Optional, but required for hector-studio to know which app to use.
	ClientID string `yaml:"client_id,omitempty"`

	// Secret is a shared secret token for simple authentication.
	// If set, requests must provide this token: Authorization: Bearer <secret>
	Secret string `yaml:"secret,omitempty"`

	// RefreshInterval is how often to refresh the JWKS.
	// Default: 15m
	RefreshInterval time.Duration `yaml:"refresh_interval,omitempty"`

	// ExcludedPaths are paths that don't require authentication.
	// Default: ["/health", "/.well-known/agent-card.json"]
	ExcludedPaths []string `yaml:"excluded_paths,omitempty"`

	// RequireAuth when true returns 401 for missing tokens.
	// When false, unauthenticated requests proceed but without user context.
	// Default: true (when Enabled is true)
	RequireAuth *bool `yaml:"require_auth,omitempty"`
}

// SetDefaults applies default values to AuthConfig.
func (c *AuthConfig) SetDefaults() {
	if c.RefreshInterval == 0 {
		c.RefreshInterval = 15 * time.Minute
	}

	if len(c.ExcludedPaths) == 0 {
		c.ExcludedPaths = []string{
			"/health",
			"/.well-known/agent-card.json",
		}
	}

	if c.RequireAuth == nil && c.Enabled {
		requireAuth := true
		c.RequireAuth = &requireAuth
	}
}

// Validate checks the AuthConfig for errors.
func (c *AuthConfig) Validate() error {
	if !c.Enabled {
		return nil // No validation needed when disabled
	}

	// Must have either JWKS (OIDC) or Secret (Shared Token)
	hasJWKS := c.JWKSURL != "" && c.Issuer != "" && c.Audience != ""
	hasSecret := c.Secret != ""

	if !hasJWKS && !hasSecret {
		return fmt.Errorf("auth enabled but no provider configured: requires either (jwks_url, issuer, audience) or (secret)")
	}

	if hasJWKS && c.RefreshInterval < time.Minute {
		return fmt.Errorf("auth.refresh_interval must be at least 1 minute")
	}

	return nil
}

// IsEnabled returns true if authentication is configured and enabled.
func (c *AuthConfig) IsEnabled() bool {
	if c == nil || !c.Enabled {
		return false
	}
	hasJWKS := c.JWKSURL != "" && c.Issuer != "" && c.Audience != ""
	hasSecret := c.Secret != ""
	return hasJWKS || hasSecret
}

// IsRequireAuth returns whether authentication is mandatory.
func (c *AuthConfig) IsRequireAuth() bool {
	if c.RequireAuth == nil {
		return c.Enabled // Default to requiring auth when enabled
	}
	return *c.RequireAuth
}

// CredentialsConfig configures credentials for outbound requests.
// Used when calling remote agents or external services.
type CredentialsConfig struct {
	// Type is the credential type: "bearer", "api_key", or "basic"
	Type string `yaml:"type,omitempty"`

	// Token is the bearer token (for type: bearer)
	Token string `yaml:"token,omitempty"`

	// APIKey is the API key (for type: api_key)
	APIKey string `yaml:"api_key,omitempty"`

	// APIKeyHeader is the header name for API key (default: X-API-Key)
	APIKeyHeader string `yaml:"api_key_header,omitempty"`

	// Username for basic auth (for type: basic)
	Username string `yaml:"username,omitempty"`

	// Password for basic auth (for type: basic)
	Password string `yaml:"password,omitempty"`
}

// SetDefaults applies default values to CredentialsConfig.
func (c *CredentialsConfig) SetDefaults() {
	if c.Type == "" {
		c.Type = "bearer"
	}
	if c.Type == "api_key" && c.APIKeyHeader == "" {
		c.APIKeyHeader = "X-API-Key"
	}
}

// Validate checks the CredentialsConfig for errors.
func (c *CredentialsConfig) Validate() error {
	if c == nil {
		return nil
	}

	switch c.Type {
	case "bearer":
		if c.Token == "" {
			return fmt.Errorf("credentials.token is required for bearer type")
		}
	case "api_key":
		if c.APIKey == "" {
			return fmt.Errorf("credentials.api_key is required for api_key type")
		}
	case "basic":
		if c.Username == "" || c.Password == "" {
			return fmt.Errorf("credentials.username and credentials.password are required for basic type")
		}
	case "":
		// No credentials configured - valid
	default:
		return fmt.Errorf("unsupported credentials.type: %s (valid: bearer, api_key, basic)", c.Type)
	}

	return nil
}
