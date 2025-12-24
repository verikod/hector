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

import "fmt"

// TriggerType identifies the trigger type.
type TriggerType string

const (
	// TriggerTypeSchedule triggers on a cron schedule.
	TriggerTypeSchedule TriggerType = "schedule"
)

// TriggerConfig configures automatic agent invocation.
type TriggerConfig struct {
	// Type of trigger (schedule).
	Type TriggerType `yaml:"type" json:"type" jsonschema:"title=Trigger Type,description=Type of trigger,enum=schedule"`

	// Enabled controls whether the trigger is active.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty" jsonschema:"title=Enabled,description=Whether the trigger is active,default=true"`

	// Cron expression for schedule triggers.
	// Uses standard cron format: minute hour day-of-month month day-of-week
	// Example: "0 9 * * *" for daily at 9am
	Cron string `yaml:"cron,omitempty" json:"cron,omitempty" jsonschema:"title=Cron Expression,description=Cron schedule (e.g. '0 9 * * *' for daily at 9am)"`

	// Timezone for schedule interpretation.
	// Default: UTC
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty" jsonschema:"title=Timezone,description=Timezone for cron schedule,default=UTC"`

	// Input is the static input to pass to the agent when triggered.
	// Should be valid JSON that will be parsed into the message content.
	Input string `yaml:"input,omitempty" json:"input,omitempty" jsonschema:"title=Input,description=Static JSON input for triggered runs"`
}

// SetDefaults applies default values.
func (c *TriggerConfig) SetDefaults() {
	if c.Enabled == nil {
		c.Enabled = BoolPtr(true)
	}
	if c.Timezone == "" {
		c.Timezone = "UTC"
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
	default:
		return fmt.Errorf("unknown trigger type: %s", c.Type)
	}

	return nil
}

// IsEnabled returns whether the trigger is enabled.
func (c *TriggerConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}
