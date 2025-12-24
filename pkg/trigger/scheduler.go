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

// Package trigger provides scheduled and event-driven agent invocation.
//
// The scheduler component uses cron expressions to trigger agents
// on a schedule without external HTTP requests.
package trigger

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/config"
)

// AgentInvoker is a function that invokes an agent with optional input.
type AgentInvoker func(ctx context.Context, agentName, input string) error

// Scheduler manages scheduled agent invocations.
type Scheduler struct {
	cron    *cron.Cron
	invoker AgentInvoker
	mu      sync.Mutex
	entries map[string]cron.EntryID // agentName -> entryID
}

// NewScheduler creates a new scheduler.
func NewScheduler(invoker AgentInvoker) *Scheduler {
	return &Scheduler{
		cron:    cron.New(cron.WithSeconds()),
		invoker: invoker,
		entries: make(map[string]cron.EntryID),
	}
}

// RegisterAgent registers an agent's trigger with the scheduler.
func (s *Scheduler) RegisterAgent(agentName string, ag agent.Agent, cfg *config.TriggerConfig) error {
	if cfg == nil || !cfg.IsEnabled() {
		return nil
	}

	if cfg.Type != config.TriggerTypeSchedule {
		return fmt.Errorf("unsupported trigger type: %s", cfg.Type)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse timezone
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", cfg.Timezone, err)
	}

	// Create cron schedule with timezone
	schedule, err := cron.ParseStandard(cfg.Cron)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cfg.Cron, err)
	}

	// Wrap schedule with timezone
	tzSchedule := &tzCronSchedule{
		schedule: schedule,
		loc:      loc,
	}

	// Create job
	input := cfg.Input
	job := cron.FuncJob(func() {
		slog.Info("Trigger firing",
			"agent", agentName,
			"schedule", cfg.Cron,
			"input", input)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := s.invoker(ctx, agentName, input); err != nil {
			slog.Error("Trigger invocation failed",
				"agent", agentName,
				"error", err)
		} else {
			slog.Info("Trigger invocation completed",
				"agent", agentName)
		}
	})

	// Add to cron
	entryID := s.cron.Schedule(tzSchedule, job)
	s.entries[agentName] = entryID

	slog.Info("Registered scheduled trigger",
		"agent", agentName,
		"cron", cfg.Cron,
		"timezone", cfg.Timezone)

	return nil
}

// Start begins the scheduler.
func (s *Scheduler) Start() {
	slog.Info("Starting trigger scheduler",
		"registered_agents", len(s.entries))
	s.cron.Start()
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() context.Context {
	slog.Info("Stopping trigger scheduler")
	return s.cron.Stop()
}

// tzCronSchedule wraps a Schedule with timezone awareness.
type tzCronSchedule struct {
	schedule cron.Schedule
	loc      *time.Location
}

func (s *tzCronSchedule) Next(t time.Time) time.Time {
	return s.schedule.Next(t.In(s.loc))
}
