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

package builder

import (
	"github.com/verikod/hector/pkg/config"
)

// CredentialsBuilder provides a fluent API for building credentials configuration.
//
// Example:
//
//	creds := builder.NewCredentials().
//	    Type("bearer").
//	    Token("my-token").
//	    Build()
type CredentialsBuilder struct {
	credType     string
	token        string
	apiKey       string
	apiKeyHeader string
	username     string
	password     string
}

// NewCredentials creates a new credentials builder.
//
// Example:
//
//	creds := builder.NewCredentials().Type("bearer").Token("token").Build()
func NewCredentials() *CredentialsBuilder {
	return &CredentialsBuilder{
		credType:     "bearer",
		apiKeyHeader: "X-API-Key",
	}
}

// Type sets the credential type: "bearer", "api_key", or "basic".
//
// Example:
//
//	builder.NewCredentials().Type("api_key")
func (b *CredentialsBuilder) Type(typ string) *CredentialsBuilder {
	b.credType = typ
	return b
}

// Token sets the bearer token (for type: bearer).
//
// Example:
//
//	builder.NewCredentials().Type("bearer").Token("my-token")
func (b *CredentialsBuilder) Token(token string) *CredentialsBuilder {
	b.token = token
	return b
}

// APIKey sets the API key (for type: api_key).
//
// Example:
//
//	builder.NewCredentials().Type("api_key").APIKey("my-key")
func (b *CredentialsBuilder) APIKey(key string) *CredentialsBuilder {
	b.apiKey = key
	return b
}

// APIKeyHeader sets the header name for API key (default: X-API-Key).
//
// Example:
//
//	builder.NewCredentials().Type("api_key").APIKeyHeader("Authorization")
func (b *CredentialsBuilder) APIKeyHeader(header string) *CredentialsBuilder {
	b.apiKeyHeader = header
	return b
}

// Username sets the username (for type: basic).
//
// Example:
//
//	builder.NewCredentials().Type("basic").Username("user")
func (b *CredentialsBuilder) Username(user string) *CredentialsBuilder {
	b.username = user
	return b
}

// Password sets the password (for type: basic).
//
// Example:
//
//	builder.NewCredentials().Type("basic").Password("pass")
func (b *CredentialsBuilder) Password(pass string) *CredentialsBuilder {
	b.password = pass
	return b
}

// Build creates the credentials configuration.
func (b *CredentialsBuilder) Build() *config.CredentialsConfig {
	creds := &config.CredentialsConfig{
		Type:         b.credType,
		Token:        b.token,
		APIKey:       b.apiKey,
		APIKeyHeader: b.apiKeyHeader,
		Username:     b.username,
		Password:     b.password,
	}
	creds.SetDefaults()
	return creds
}
