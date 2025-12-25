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

// Package notification provides outbound notification dispatching.
package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"text/template"
	"time"

	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/httpclient"
	"github.com/verikod/hector/pkg/utils"
)

// Event represents a notification event with context data.
type Event struct {
	Type      config.NotificationEvent `json:"type"`
	AgentName string                   `json:"agent_name"`
	TaskID    string                   `json:"task_id,omitempty"`
	Status    string                   `json:"status"`
	Result    string                   `json:"result,omitempty"`
	Error     string                   `json:"error,omitempty"`
	Timestamp time.Time                `json:"timestamp"`
	Metadata  map[string]any           `json:"metadata,omitempty"`
}

// Notifier dispatches notifications for agent events.
type Notifier struct {
	// Map of agent name -> notification configs
	configs map[string][]*config.NotificationConfig
}

// New creates a new Notifier from configuration.
func New(cfg *config.Config) *Notifier {
	if cfg == nil {
		return nil
	}

	configs := make(map[string][]*config.NotificationConfig)

	// Collect notifications from all agents
	for name, agentCfg := range cfg.Agents {
		if agentCfg == nil || len(agentCfg.Notifications) == 0 {
			continue
		}

		var enabledNotifications []*config.NotificationConfig
		for _, notifCfg := range agentCfg.Notifications {
			if notifCfg == nil || !notifCfg.IsEnabled() {
				continue
			}
			notifCfg.SetDefaults()
			enabledNotifications = append(enabledNotifications, notifCfg)
		}

		if len(enabledNotifications) > 0 {
			configs[name] = enabledNotifications
			slog.Debug("Registered agent notifications",
				"agent", name,
				"count", len(enabledNotifications))
		}
	}

	if len(configs) == 0 {
		return nil
	}

	return &Notifier{
		configs: configs,
	}
}

// Notify dispatches notifications for an event.
// This should be called from agent callbacks or runner hooks.
func (n *Notifier) Notify(ctx context.Context, event Event) {
	if n == nil || n.configs == nil {
		return
	}

	configs, ok := n.configs[event.AgentName]
	if !ok {
		return
	}

	for _, cfg := range configs {
		if !cfg.HasEvent(event.Type) {
			continue
		}

		// Dispatch notification asynchronously
		go n.sendNotification(ctx, cfg, event)
	}
}

// NotifyTaskCompleted is a convenience method for task completion events.
func (n *Notifier) NotifyTaskCompleted(agentName, taskID, result string) {
	n.Notify(context.Background(), Event{
		Type:      config.NotificationEventTaskCompleted,
		AgentName: agentName,
		TaskID:    taskID,
		Status:    "completed",
		Result:    result,
		Timestamp: time.Now(),
	})
}

// NotifyTaskFailed is a convenience method for task failure events.
func (n *Notifier) NotifyTaskFailed(agentName, taskID, errorMsg string) {
	n.Notify(context.Background(), Event{
		Type:      config.NotificationEventTaskFailed,
		AgentName: agentName,
		TaskID:    taskID,
		Status:    "failed",
		Error:     errorMsg,
		Timestamp: time.Now(),
	})
}

// NotifyTaskStarted is a convenience method for task start events.
func (n *Notifier) NotifyTaskStarted(agentName, taskID string) {
	n.Notify(context.Background(), Event{
		Type:      config.NotificationEventTaskStarted,
		AgentName: agentName,
		TaskID:    taskID,
		Status:    "started",
		Timestamp: time.Now(),
	})
}

// sendNotification sends a single notification using httpclient with built-in retries.
func (n *Notifier) sendNotification(ctx context.Context, cfg *config.NotificationConfig, event Event) {
	// Build payload
	payload, err := n.buildPayload(cfg, event)
	if err != nil {
		slog.Error("Failed to build notification payload",
			"notification_id", cfg.ID,
			"agent", event.AgentName,
			"error", err)
		return
	}

	// Configure httpclient with retry settings from notification config
	retry := cfg.Retry
	if retry == nil {
		retry = &config.NotificationRetryConfig{}
		retry.SetDefaults()
	}

	// Use Hector's httpclient with built-in retry/backoff
	client := httpclient.New(
		httpclient.WithMaxRetries(retry.MaxAttempts),
		httpclient.WithBaseDelay(retry.InitialDelay),
		httpclient.WithMaxDelay(retry.MaxDelay),
		httpclient.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
	)

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(payload))
	if err != nil {
		slog.Error("Failed to create notification request",
			"notification_id", cfg.ID,
			"error", err)
		return
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Hector-Notification/1.0")
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	// Send with built-in retries
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Notification failed after retries",
			"notification_id", cfg.ID,
			"agent", event.AgentName,
			"event", event.Type,
			"error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Error("Notification returned error status",
			"notification_id", cfg.ID,
			"agent", event.AgentName,
			"status", resp.StatusCode)
		return
	}

	slog.Info("Notification sent successfully",
		"notification_id", cfg.ID,
		"agent", event.AgentName,
		"event", event.Type)
}

// buildPayload generates the webhook payload from template or default.
func (n *Notifier) buildPayload(cfg *config.NotificationConfig, event Event) ([]byte, error) {
	if cfg.Payload != nil && cfg.Payload.Template != "" {
		// Use custom template
		tmpl, err := template.New("payload").Funcs(utils.TemplateFuncs()).Parse(cfg.Payload.Template)
		if err != nil {
			return nil, fmt.Errorf("invalid payload template: %w", err)
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, event); err != nil {
			return nil, fmt.Errorf("template execution failed: %w", err)
		}

		return buf.Bytes(), nil
	}

	// Default: JSON-encode the event
	return json.Marshal(event)
}
