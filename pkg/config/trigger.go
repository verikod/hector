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

// TriggerType identifies the trigger type.
type TriggerType string

const (
	// TriggerTypeSchedule triggers on a cron schedule.
	TriggerTypeSchedule TriggerType = "schedule"

	// TriggerTypeWebhook triggers on incoming HTTP webhook requests.
	TriggerTypeWebhook TriggerType = "webhook"
)

// WebhookResponseMode defines how webhook triggers respond to requests.
type WebhookResponseMode string

const (
	// WebhookResponseSync waits for agent completion before responding.
	WebhookResponseSync WebhookResponseMode = "sync"

	// WebhookResponseAsync returns task ID immediately for polling.
	WebhookResponseAsync WebhookResponseMode = "async"

	// WebhookResponseCallback returns immediately and POSTs result to callback URL.
	WebhookResponseCallback WebhookResponseMode = "callback"
)

// WebhookInputConfig configures how webhook payloads are transformed to agent input.
type WebhookInputConfig struct {
	// Template is a Go text/template for transforming the webhook payload.
	// Available data: .body (parsed JSON), .headers (HTTP headers), .query (URL query params)
	// Example: "Process order {{.body.order_id}} from {{.headers.X-Shop-Domain}}"
	Template string `yaml:"template,omitempty" json:"template,omitempty" jsonschema:"title=Input Template,description=Go template for transforming webhook payload to agent input"`

	// ExtractFields extracts specific fields from the payload as template variables.
	ExtractFields []WebhookFieldExtractor `yaml:"extract_fields,omitempty" json:"extract_fields,omitempty" jsonschema:"title=Extract Fields,description=Fields to extract from payload"`
}

// WebhookFieldExtractor defines a field to extract from webhook payload.
type WebhookFieldExtractor struct {
	// Path is the JSONPath-like path to the field (e.g., ".body.order.id")
	Path string `yaml:"path" json:"path" jsonschema:"title=Field Path,description=Path to field in payload"`

	// As is the name to use for this field in the template context.
	As string `yaml:"as" json:"as" jsonschema:"title=Field Alias,description=Name to use in template"`
}

// WebhookResponseConfig configures webhook response behavior.
type WebhookResponseConfig struct {
	// Mode determines how the webhook responds (sync, async, callback).
	// Default: sync
	Mode WebhookResponseMode `yaml:"mode,omitempty" json:"mode,omitempty" jsonschema:"title=Response Mode,description=How to respond to webhook requests,enum=sync,enum=async,enum=callback,default=sync"`

	// Timeout is the maximum time to wait for agent completion in sync mode.
	// Default: 30s
	Timeout time.Duration `yaml:"timeout,omitempty" json:"timeout,omitempty" jsonschema:"title=Timeout,description=Max wait time for sync mode,default=30s"`

	// CallbackField is the payload field containing the callback URL (for callback mode).
	// Default: "callback_url"
	CallbackField string `yaml:"callback_field,omitempty" json:"callback_field,omitempty" jsonschema:"title=Callback Field,description=Field containing callback URL,default=callback_url"`
}

// TriggerConfig configures automatic agent invocation.
type TriggerConfig struct {
	// Type of trigger (schedule, webhook).
	Type TriggerType `yaml:"type" json:"type" jsonschema:"title=Trigger Type,description=Type of trigger,enum=schedule,enum=webhook"`

	// Enabled controls whether the trigger is active.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty" jsonschema:"title=Enabled,description=Whether the trigger is active,default=true"`

	// --- Schedule trigger fields ---

	// Cron expression for schedule triggers.
	// Uses standard cron format: minute hour day-of-month month day-of-week
	// Example: "0 9 * * *" for daily at 9am
	Cron string `yaml:"cron,omitempty" json:"cron,omitempty" jsonschema:"title=Cron Expression,description=Cron schedule (e.g. '0 9 * * *' for daily at 9am)"`

	// Timezone for schedule interpretation.
	// Default: UTC
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty" jsonschema:"title=Timezone,description=Timezone for cron schedule,default=UTC"`

	// Input is the static input to pass to the agent when triggered.
	// For schedule triggers, this is the fixed input.
	// For webhook triggers, this is only used if no WebhookInput template is configured.
	Input string `yaml:"input,omitempty" json:"input,omitempty" jsonschema:"title=Input,description=Static input for triggered runs"`

	// --- Webhook trigger fields ---

	// Path is the URL path for the webhook endpoint.
	// Default: /webhooks/{agent-name}
	Path string `yaml:"path,omitempty" json:"path,omitempty" jsonschema:"title=Webhook Path,description=URL path for webhook endpoint"`

	// Methods are the allowed HTTP methods for the webhook.
	// Default: ["POST"]
	Methods []string `yaml:"methods,omitempty" json:"methods,omitempty" jsonschema:"title=HTTP Methods,description=Allowed HTTP methods,default=[POST]"`

	// Secret is the HMAC secret for signature verification.
	// If set, requests must include a valid signature.
	Secret string `yaml:"secret,omitempty" json:"secret,omitempty" jsonschema:"title=HMAC Secret,description=Secret for webhook signature verification"`

	// SignatureHeader is the HTTP header containing the request signature.
	// Common values: X-Hub-Signature-256 (GitHub), X-Shopify-Hmac-Sha256 (Shopify)
	// Default: X-Webhook-Signature
	SignatureHeader string `yaml:"signature_header,omitempty" json:"signature_header,omitempty" jsonschema:"title=Signature Header,description=HTTP header containing HMAC signature,default=X-Webhook-Signature"`

	// WebhookInput configures how webhook payloads are transformed to agent input.
	WebhookInput *WebhookInputConfig `yaml:"webhook_input,omitempty" json:"webhook_input,omitempty" jsonschema:"title=Webhook Input,description=Payload transformation configuration"`

	// Response configures webhook response behavior.
	Response *WebhookResponseConfig `yaml:"response,omitempty" json:"response,omitempty" jsonschema:"title=Response Config,description=Webhook response configuration"`
}

// SetDefaults applies default values.
func (c *TriggerConfig) SetDefaults() {
	if c.Enabled == nil {
		c.Enabled = BoolPtr(true)
	}

	// Schedule trigger defaults
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}

	// Webhook trigger defaults
	if c.Type == TriggerTypeWebhook {
		if len(c.Methods) == 0 {
			c.Methods = []string{"POST"}
		}
		if c.SignatureHeader == "" {
			c.SignatureHeader = "X-Webhook-Signature"
		}
		if c.Response == nil {
			c.Response = &WebhookResponseConfig{}
		}
		if c.Response.Mode == "" {
			c.Response.Mode = WebhookResponseSync
		}
		if c.Response.Timeout == 0 {
			c.Response.Timeout = 30 * time.Second
		}
		if c.Response.CallbackField == "" {
			c.Response.CallbackField = "callback_url"
		}
	}
}

// Validate checks the trigger configuration.
func (c *TriggerConfig) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("trigger type is required")
	}

	switch c.Type {
	case TriggerTypeSchedule:
		if c.Cron == "" {
			return fmt.Errorf("cron expression is required for schedule trigger")
		}
	case TriggerTypeWebhook:
		// Webhook triggers are valid as-is; path defaults to /webhooks/{agent-name}
		// Validate response mode if set
		if c.Response != nil && c.Response.Mode != "" {
			switch c.Response.Mode {
			case WebhookResponseSync, WebhookResponseAsync, WebhookResponseCallback:
				// Valid
			default:
				return fmt.Errorf("invalid webhook response mode: %s", c.Response.Mode)
			}
		}
	default:
		return fmt.Errorf("unknown trigger type: %s", c.Type)
	}

	return nil
}

// IsEnabled returns whether the trigger is enabled.
func (c *TriggerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}
