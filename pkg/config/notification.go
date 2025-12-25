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

// NotificationType identifies the notification delivery method.
type NotificationType string

const (
	// NotificationTypeWebhook sends notifications via HTTP webhook.
	NotificationTypeWebhook NotificationType = "webhook"
)

// NotificationEvent represents events that can trigger notifications.
type NotificationEvent string

const (
	// NotificationEventTaskCompleted fires when an agent task completes successfully.
	NotificationEventTaskCompleted NotificationEvent = "task.completed"

	// NotificationEventTaskFailed fires when an agent task fails.
	NotificationEventTaskFailed NotificationEvent = "task.failed"

	// NotificationEventTaskStarted fires when an agent task starts.
	NotificationEventTaskStarted NotificationEvent = "task.started"
)

// NotificationConfig configures an outbound notification.
type NotificationConfig struct {
	// ID is the unique identifier for this notification.
	ID string `yaml:"id" json:"id" jsonschema:"title=Notification ID,description=Unique identifier for this notification"`

	// Type of notification (webhook).
	Type NotificationType `yaml:"type" json:"type" jsonschema:"title=Type,description=Notification type,enum=webhook,default=webhook"`

	// Enabled controls whether this notification is active.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty" jsonschema:"title=Enabled,description=Whether the notification is active,default=true"`

	// Events that trigger this notification.
	Events []NotificationEvent `yaml:"events" json:"events" jsonschema:"title=Events,description=Events that trigger notification"`

	// --- Webhook-specific fields ---

	// URL is the webhook endpoint to send notifications to.
	URL string `yaml:"url" json:"url" jsonschema:"title=Webhook URL,description=URL to send notifications to"`

	// Headers are additional HTTP headers to include.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty" jsonschema:"title=Headers,description=Additional HTTP headers"`

	// Payload configures the notification payload.
	Payload *NotificationPayloadConfig `yaml:"payload,omitempty" json:"payload,omitempty" jsonschema:"title=Payload,description=Payload configuration"`

	// Retry configures retry behavior for failed notifications.
	Retry *NotificationRetryConfig `yaml:"retry,omitempty" json:"retry,omitempty" jsonschema:"title=Retry,description=Retry configuration"`
}

// NotificationPayloadConfig configures notification payload generation.
type NotificationPayloadConfig struct {
	// Template is a Go text/template for generating the payload.
	// Available data: .event, .agent_name, .task_id, .status, .result, .error, .timestamp
	Template string `yaml:"template,omitempty" json:"template,omitempty" jsonschema:"title=Payload Template,description=Go template for payload generation"`
}

// NotificationRetryConfig configures retry behavior.
type NotificationRetryConfig struct {
	// MaxAttempts is the maximum number of retry attempts.
	// Default: 3
	MaxAttempts int `yaml:"max_attempts,omitempty" json:"max_attempts,omitempty" jsonschema:"title=Max Attempts,description=Maximum retry attempts,default=3"`

	// InitialDelay is the initial delay before first retry.
	// Default: 1s
	InitialDelay time.Duration `yaml:"initial_delay,omitempty" json:"initial_delay,omitempty" jsonschema:"title=Initial Delay,description=Initial retry delay,default=1s"`

	// MaxDelay is the maximum delay between retries.
	// Default: 30s
	MaxDelay time.Duration `yaml:"max_delay,omitempty" json:"max_delay,omitempty" jsonschema:"title=Max Delay,description=Maximum retry delay,default=30s"`
}

// SetDefaults applies default values.
func (c *NotificationConfig) SetDefaults() {
	if c.Enabled == nil {
		c.Enabled = BoolPtr(true)
	}
	if c.Type == "" {
		c.Type = NotificationTypeWebhook
	}
	if c.Retry == nil {
		c.Retry = &NotificationRetryConfig{}
	}
	c.Retry.SetDefaults()
}

// SetDefaults applies default values for retry config.
func (c *NotificationRetryConfig) SetDefaults() {
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 3
	}
	if c.InitialDelay == 0 {
		c.InitialDelay = 1 * time.Second
	}
	if c.MaxDelay == 0 {
		c.MaxDelay = 30 * time.Second
	}
}

// Validate checks the notification configuration.
func (c *NotificationConfig) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("notification id is required")
	}
	if c.Type == "" {
		return fmt.Errorf("notification type is required")
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("at least one event is required")
	}

	switch c.Type {
	case NotificationTypeWebhook:
		if c.URL == "" {
			return fmt.Errorf("webhook url is required")
		}
	default:
		return fmt.Errorf("unknown notification type: %s", c.Type)
	}

	// Validate events
	for _, event := range c.Events {
		switch event {
		case NotificationEventTaskCompleted, NotificationEventTaskFailed, NotificationEventTaskStarted:
			// Valid
		default:
			return fmt.Errorf("unknown notification event: %s", event)
		}
	}

	return nil
}

// IsEnabled returns whether the notification is enabled.
func (c *NotificationConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

// HasEvent checks if the notification is configured for a specific event.
func (c *NotificationConfig) HasEvent(event NotificationEvent) bool {
	for _, e := range c.Events {
		if e == event {
			return true
		}
	}
	return false
}
