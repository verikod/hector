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

package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2agrpc"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"

	"github.com/verikod/hector/pkg/auth"
	"github.com/verikod/hector/pkg/config"
	"github.com/verikod/hector/pkg/observability"
	"github.com/verikod/hector/pkg/task"
	"github.com/verikod/hector/pkg/trigger"
	"google.golang.org/grpc"
)

// webUIHTML removed (headless server)

// HTTPServer is the Hector HTTP server.
// Uses a2a-go native handlers for A2A protocol compliance.
type HTTPServer struct {
	serverCfg *config.ServerConfig
	appCfg    *config.Config
	server    *http.Server

	// gRPC server (only when Transport == TransportGRPC)
	grpcServer *grpc.Server
	//nolint:unused // Reserved for future use
	grpcListener net.Listener

	// TaskStore for persistent task storage (nil = in-memory)
	taskStore a2asrv.TaskStore

	// Auth: JWT validator and a2a-go interceptor
	authValidator   auth.TokenValidator
	authInterceptor *auth.Interceptor

	// Observability: tracing and metrics
	observability *observability.Manager

	// Task service for task-scoped cancellation
	taskService task.Service

	// Per-agent: JSON-RPC handler + agent card handler (both from a2a-go)
	agentJSONRPCHandlers map[string]http.Handler
	agentCardHandlers    map[string]http.Handler
	agentCards           map[string]*a2a.AgentCard

	// Per-agent: gRPC handlers (only when Transport == TransportGRPC)
	agentGRPCHandlers map[string]*a2agrpc.Handler

	// Webhook handlers for webhook-triggered agents
	webhookHandlers map[string]*trigger.WebhookHandler

	// Studio mode: config file path and studio mode flag
	configPath string
	studioMode bool
	reloadFunc func() error // Called to trigger synchronous config reload
	mu         sync.RWMutex
}

// HTTPServerOption configures the HTTP server.
type HTTPServerOption func(*HTTPServer)

// WithTaskStore sets the task store for persistent task storage.
// If not set, a2a-go uses its internal in-memory store.
func WithTaskStore(store a2asrv.TaskStore) HTTPServerOption {
	return func(s *HTTPServer) {
		s.taskStore = store
	}
}

// WithAuthValidator sets the JWT validator for authentication.
// When set, HTTP requests will be validated and claims passed to agents.
func WithAuthValidator(validator auth.TokenValidator) HTTPServerOption {
	return func(s *HTTPServer) {
		s.authValidator = validator
	}
}

// WithObservability sets the observability manager for tracing and metrics.
func WithObservability(obs *observability.Manager) HTTPServerOption {
	return func(s *HTTPServer) {
		s.observability = obs
	}
}

// WithTaskService sets the task service for task-scoped cancellation.
func WithTaskService(ts task.Service) HTTPServerOption {
	return func(s *HTTPServer) {
		s.taskService = ts
	}
}

// WithWebhookHandlers sets the webhook handlers for webhook-triggered agents.
func WithWebhookHandlers(handlers map[string]*trigger.WebhookHandler) HTTPServerOption {
	return func(s *HTTPServer) {
		s.webhookHandlers = handlers
	}
}

// NewHTTPServer creates a new HTTP server from config.
// executors is a map of agent name to its executor (one per agent).
func NewHTTPServer(appCfg *config.Config, executors map[string]*Executor, opts ...HTTPServerOption) *HTTPServer {
	serverCfg := &appCfg.Server

	if serverCfg.Host == "" || serverCfg.Port == 0 {
		serverCfg.SetDefaults()
	}

	s := &HTTPServer{
		serverCfg:            serverCfg,
		appCfg:               appCfg,
		agentJSONRPCHandlers: make(map[string]http.Handler),
		agentCardHandlers:    make(map[string]http.Handler),
		agentCards:           make(map[string]*a2a.AgentCard),
		agentGRPCHandlers:    make(map[string]*a2agrpc.Handler),
	}

	// Apply options
	for _, opt := range opts {
		opt(s)
	}

	// Build handlers using a2a-go native functions
	s.buildAgentHandlers(executors)

	return s
}

// buildAgentHandlers creates a2a-go native handlers for each configured agent.
func (s *HTTPServer) buildAgentHandlers(executors map[string]*Executor) {
	baseURL := "http://" + s.serverCfg.Address()

	// Create auth interceptor if validator is configured.
	// IMPORTANT: RequireAuth is set to FALSE here because:
	// 1. /agents/ paths are excluded from HTTP auth middleware to support per-agent visibility
	// 2. Visibility-based auth (public/internal/private) is handled in handleAgentRoutes
	// 3. The interceptor only bridges Claims to CallContext when available, it doesn't enforce auth
	if s.authValidator != nil {
		s.authInterceptor = auth.NewInterceptor(false) // Don't require auth - visibility handles it
	}

	for name, agentCfg := range s.appCfg.Agents {
		// Build A2A AgentCard
		agentURL := baseURL + "/agents/" + name
		card := s.buildAgentCard(name, agentCfg, agentURL)
		s.agentCards[name] = card

		// Get per-agent executor
		executor, ok := executors[name]
		if !ok {
			slog.Warn("No executor for agent, skipping", "agent", name)
			continue
		}

		// Create a2a-go native JSON-RPC handler with agent's executor
		// Include TaskStore if configured for persistent task storage
		var handlerOpts []a2asrv.RequestHandlerOption
		if s.taskStore != nil {
			handlerOpts = append(handlerOpts, a2asrv.WithTaskStore(s.taskStore))
		}

		// Add auth interceptor to bridge HTTP auth to a2a-go CallContext
		if s.authInterceptor != nil {
			handlerOpts = append(handlerOpts, a2asrv.WithCallInterceptor(s.authInterceptor))
		}

		requestHandler := a2asrv.NewHandler(executor, handlerOpts...)

		// Create transport-specific handlers based on config
		if s.serverCfg.Transport == config.TransportGRPC {
			// Create gRPC handler
			s.agentGRPCHandlers[name] = a2agrpc.NewHandler(requestHandler)
		} else {
			// Create JSON-RPC handler (default)
			s.agentJSONRPCHandlers[name] = a2asrv.NewJSONRPCHandler(requestHandler)
		}

		// Create a2a-go native agent card handler
		s.agentCardHandlers[name] = a2asrv.NewStaticAgentCardHandler(card)
	}
}

// buildAgentCard creates an A2A-compliant agent card.
func (s *HTTPServer) buildAgentCard(name string, cfg *config.AgentConfig, url string) *a2a.AgentCard {
	// Ensure input/output modes have defaults
	inputModes := cfg.InputModes
	if len(inputModes) == 0 {
		inputModes = []string{"text/plain"}
	}
	outputModes := cfg.OutputModes
	if len(outputModes) == 0 {
		outputModes = []string{"text/plain"}
	}

	// Determine display name: explicit name > map key
	displayName := cfg.Name
	if displayName == "" {
		displayName = name
	}

	// Build skills
	skills := s.buildAgentSkills(cfg)
	if len(skills) == 0 {
		skills = []a2a.AgentSkill{{
			ID:          name,
			Name:        displayName,
			Description: cfg.Description,
			Tags:        []string{"general", "assistant"},
		}}
	}

	// Version handling
	version := s.appCfg.Version
	if version == "" {
		version = "2.0.0-alpha"
	}

	card := &a2a.AgentCard{
		Name:               displayName,
		Description:        cfg.Description,
		URL:                url,
		Version:            version,
		ProtocolVersion:    "1.0",
		DefaultInputModes:  inputModes,
		DefaultOutputModes: outputModes,
		Skills:             skills,
		Capabilities: a2a.AgentCapabilities{
			Streaming:              true,
			PushNotifications:      false,
			StateTransitionHistory: false,
		},
		PreferredTransport: a2a.TransportProtocolJSONRPC,
		Provider: &a2a.AgentProvider{
			Org: "Hector",
			URL: "https://github.com/verikod/hector",
		},
	}

	// Add security schemes when auth is enabled (A2A spec section 5.5)
	if s.authValidator != nil && s.serverCfg.Auth != nil && s.serverCfg.Auth.IsEnabled() {
		card.SecuritySchemes = a2a.NamedSecuritySchemes{
			"BearerAuth": a2a.HTTPAuthSecurityScheme{
				Scheme:       "bearer",
				BearerFormat: "JWT",
				Description:  "JWT Bearer token authentication",
			},
		}
		card.Security = []a2a.SecurityRequirements{
			{"BearerAuth": a2a.SecuritySchemeScopes{}},
		}
	}

	return card
}

// buildAgentSkills converts config skills to A2A skills.
func (s *HTTPServer) buildAgentSkills(cfg *config.AgentConfig) []a2a.AgentSkill {
	var skills []a2a.AgentSkill
	for _, skill := range cfg.Skills {
		skills = append(skills, a2a.AgentSkill{
			ID:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        skill.Tags,
			Examples:    skill.Examples,
		})
	}
	return skills
}

// Start starts the HTTP server.
func (s *HTTPServer) Start(ctx context.Context) error {
	mux := s.setupRoutes()

	// Apply middleware chain (order: observability -> logging -> cors -> auth -> routes)
	// Observability wraps everything so all requests are traced/measured
	var handler http.Handler = mux

	// Auth middleware: validates JWT and stores claims in context
	// Must be applied before CORS so OPTIONS preflight requests pass through
	if s.authValidator != nil {
		excludedPaths := []string{"/health", "/.well-known/agent-card.json", "/agents", "/agents/"}
		if s.serverCfg.Auth != nil && len(s.serverCfg.Auth.ExcludedPaths) > 0 {
			excludedPaths = s.serverCfg.Auth.ExcludedPaths
			// Ensure defaults are always preserved unless explicitly overridden?
			// Usually we append, but here the config overrides.
			// Re-add critical system paths if they were overwritten by config
			// Note: User config should control this, but we need /agents/ excluded to support public visibility logic
			// If user provides custom list, they might lock themselves out of public agents if they don't include /agents/
			// Safest approach: Append critical internal exclusions to user list
			defaults := []string{"/agents", "/agents/"}
			for _, d := range defaults {
				found := false
				for _, u := range excludedPaths {
					if u == d {
						found = true
						break
					}
				}
				if !found {
					excludedPaths = append(excludedPaths, d)
				}
			}
		}
		// Also exclude metrics endpoint from auth
		if s.observability != nil && s.observability.MetricsEnabled() {
			excludedPaths = append(excludedPaths, s.observability.MetricsEndpoint())
		}
		handler = auth.MiddlewareWithExclusions(s.authValidator, excludedPaths)(handler)
		slog.Info("Authentication enabled", "excluded_paths", excludedPaths)
	}

	handler = s.corsMiddleware(handler)
	handler = s.loggingMiddleware(handler)

	// Observability middleware (outermost for complete request coverage)
	if s.observability != nil {
		handler = observability.HTTPMiddleware(s.observability.Tracer(), s.observability.Metrics())(handler)
	}

	s.server = &http.Server{
		Addr:         s.serverCfg.Address(),
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("HTTP server starting", "address", s.serverCfg.Address())

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return s.Shutdown(context.Background())
	}
}

// Shutdown gracefully shuts down the server(s).
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var errs []error

	// Shutdown HTTP server
	if s.server != nil {
		slog.Info("HTTP server shutting down")
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("HTTP shutdown error: %w", err))
		}
	}

	// Shutdown gRPC server
	if s.grpcServer != nil {
		slog.Info("gRPC server shutting down")
		stopped := make(chan struct{})
		go func() {
			s.grpcServer.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			slog.Info("gRPC server stopped gracefully")
		case <-shutdownCtx.Done():
			slog.Warn("gRPC graceful stop timeout, forcing shutdown")
			s.grpcServer.Stop()
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}

	return nil
}

// Address returns the HTTP server address.
func (s *HTTPServer) Address() string {
	return s.serverCfg.Address()
}

// GRPCAddress returns the gRPC server address (if enabled).
func (s *HTTPServer) GRPCAddress() string {
	if s.serverCfg.Transport == config.TransportGRPC {
		return s.serverCfg.GRPCAddress()
	}
	return ""
}

// setupRoutes configures the HTTP routes.
// A2A spec compliant paths:
//   - GET  /.well-known/agent-card.json  → Default agent card (a2a-go native)
//   - GET  /agents                       → Discovery (Hector extension)
//   - GET  /agents/{name}                → Agent card (a2a-go native)
//   - POST /agents/{name}                → JSON-RPC (a2a-go native)
//   - GET  /agents/{name}/.well-known/agent-card.json → Agent card (a2a-go native)
func (s *HTTPServer) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Web UI removed (headless) - use hector-studio
	// mux.HandleFunc("/", s.handleRoot)

	// Health check
	mux.HandleFunc("/health", s.handleHealth)

	// Schema endpoint for config builder UI
	mux.HandleFunc("/api/schema", s.handleGetSchema)

	// Config endpoints (studio mode)
	mux.HandleFunc("/api/config", s.handleConfigEndpoint)

	// Prometheus metrics endpoint (if enabled)
	if s.observability != nil && s.observability.MetricsEnabled() {
		metricsEndpoint := s.observability.MetricsEndpoint()
		mux.Handle(metricsEndpoint, s.observability.MetricsHandler())
		slog.Info("Metrics endpoint enabled", "path", metricsEndpoint)
	}

	// A2A spec: server-level well-known agent card (returns first/default agent)
	// This is what single-agent clients expect per spec section 5.3
	mux.HandleFunc(a2asrv.WellKnownAgentCardPath, s.handleDefaultAgentCard)

	// Agent discovery - Hector extension (returns all agent cards)
	mux.HandleFunc("/agents", s.handleDiscovery)

	// Per-agent routes using a2a-go native handlers
	mux.HandleFunc("/agents/", s.handleAgentRoutes)

	// Tool cancellation endpoint (Hector extension)
	// Pattern: /api/tasks/{taskId}/toolCalls/{callId}/cancel
	mux.HandleFunc("/api/tasks/", s.handleTasksAPI)

	// Webhook trigger routes
	s.registerWebhookRoutes(mux)

	return mux
}

// registerWebhookRoutes registers webhook handlers at their configured paths.
func (s *HTTPServer) registerWebhookRoutes(mux *http.ServeMux) {
	if len(s.webhookHandlers) == 0 {
		return
	}

	for agentName, handler := range s.webhookHandlers {
		// Get the configured path from the agent's trigger config
		agentCfg, ok := s.appCfg.Agents[agentName]
		if !ok || agentCfg.Trigger == nil {
			continue
		}

		path := agentCfg.Trigger.Path
		if path == "" {
			// Default path: /webhooks/{agent-name}
			path = "/webhooks/" + agentName
		}

		mux.Handle(path, handler)
		slog.Info("Webhook endpoint registered",
			"agent", agentName,
			"path", path,
			"methods", agentCfg.Trigger.Methods)
	}
}

// handleRoot removed (headless server)

// handleHealth returns server health status.
// Also provides auth discovery information for remote clients like hector-studio.
func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	studioMode := s.studioMode
	s.mu.RUnlock()

	response := map[string]any{
		"status":      "ok",
		"studio_mode": studioMode,
	}

	// Auth discovery for clients (e.g., hector-studio)
	if s.serverCfg.Auth != nil && s.serverCfg.Auth.IsEnabled() {
		response["auth"] = map[string]any{
			"enabled":   true,
			"type":      "jwt",
			"issuer":    s.serverCfg.Auth.Issuer,
			"audience":  s.serverCfg.Auth.Audience,
			"client_id": s.serverCfg.Auth.ClientID,
		}
	}

	// Studio config info
	if s.serverCfg.Studio != nil && s.serverCfg.Studio.IsEnabled() {
		response["studio"] = map[string]any{
			"enabled":       true,
			"allowed_roles": s.serverCfg.Studio.GetAllowedRoles(),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// handleTasksAPI handles task-related API endpoints.
// POST /api/tasks/{taskId}/toolCalls/{callId}/cancel - Cancel a specific tool execution
func (s *HTTPServer) handleTasksAPI(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/tasks/{taskId}/toolCalls/{callId}/cancel
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	parts := strings.Split(path, "/")

	// Expect: {taskId}/toolCalls/{callId}/cancel
	if len(parts) >= 4 && parts[1] == "toolCalls" && parts[3] == "cancel" {
		s.handleCancelToolCall(w, r, parts[0], parts[2])
		return
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// handleCancelToolCall cancels a specific tool execution within a task.
// POST /api/tasks/{taskId}/toolCalls/{callId}/cancel
func (s *HTTPServer) handleCancelToolCall(w http.ResponseWriter, r *http.Request, taskID, callID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if taskID == "" || callID == "" {
		http.Error(w, "taskId and callId are required", http.StatusBadRequest)
		return
	}

	// Check if task service is configured
	if s.taskService == nil {
		http.Error(w, "Task cancellation not available", http.StatusServiceUnavailable)
		return
	}

	// Get task from service
	taskObj, err := s.taskService.Get(r.Context(), taskID)
	if err != nil {
		if err == task.ErrTaskNotFound {
			http.Error(w, "Task not found", http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	// Cancel the specific tool execution
	// UI widget IDs are prefixed with "tool_" but backend uses raw callID
	actualCallID := callID
	if strings.HasPrefix(callID, "tool_") {
		actualCallID = strings.TrimPrefix(callID, "tool_")
	}
	cancelled := taskObj.CancelExecution(actualCallID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cancelled": cancelled,
		"task_id":   taskID,
		"call_id":   callID,
	})

	if cancelled {
		slog.Info("Tool execution cancelled", "task_id", taskID, "call_id", callID)
	} else {
		slog.Debug("Tool cancellation failed - not found or already completed", "task_id", taskID, "call_id", callID)
	}
}

// handleGetSchema generates and returns JSON Schema for the config builder UI.
// Schema is generated dynamically to ensure it's always current and works in
// production deployments where file paths may not be available.
func (s *HTTPServer) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reflector := &jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true, // Inline all definitions for @rjsf/core compatibility
		// IMPORTANT: Use YAML field names (snake_case) instead of Go field names (PascalCase)
		// This ensures the schema matches the actual YAML structure
		FieldNameTag: "yaml",
	}

	schema := reflector.Reflect(&config.Config{})

	// Add metadata
	schema.ID = "https://hector.dev/schemas/config.json"
	schema.Title = "Hector Configuration Schema"
	schema.Description = "Complete configuration schema for Hector v2 agent framework"
	schema.Version = "http://json-schema.org/draft-07/schema#"

	// Set headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Write response
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(schema); err != nil {
		slog.Error("Failed to encode schema", "error", err)
		http.Error(w, "Failed to generate schema", http.StatusInternalServerError)
		return
	}
}

// handleDefaultAgentCard serves the default agent's card at the server-level well-known path.
// Per A2A spec 5.3: "https://{server_domain}/.well-known/agent-card.json"
// For multi-agent servers, this returns the first configured agent.
func (s *HTTPServer) handleDefaultAgentCard(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Get the first/default agent's card handler
	for _, handler := range s.agentCardHandlers {
		handler.ServeHTTP(w, r)
		return
	}
	http.Error(w, "No agents configured", http.StatusNotFound)
}

// handleDiscovery returns all agents (Hector extension).
// Filters agents based on their visibility configuration and authentication status.
func (s *HTTPServer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check authentication (soft check)
	isAuthenticated := false
	if s.authValidator != nil {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if _, err := s.authValidator.ValidateToken(r.Context(), token); err == nil {
				isAuthenticated = true
			}
		}
	} else {
		// No auth configured = effectively authenticated (or no auth concept)
		// But in "visibility" context, if auth is disabled, "internal" implies "hidden from public"?
		// Or "internal" assumes trusted network?
		// Common pattern: If Auth OFF, everything is public/trusted.
		isAuthenticated = true
	}

	agents := make([]*a2a.AgentCard, 0, len(s.agentCards))
	for name, card := range s.agentCards {
		cfg, ok := s.appCfg.Agents[name]
		if !ok {
			continue // Should not happen
		}

		visibility := cfg.Visibility
		if visibility == "" {
			visibility = "public"
		}

		switch visibility {
		case "public":
			// Always visible
			agents = append(agents, card)
		case "internal":
			// Visible only if authenticated
			if isAuthenticated {
				agents = append(agents, card)
			}
		case "private":
			// Never visible in discovery
			// (Hidden from list, but potentially callable internally)
		default:
			// Default to public behavior
			agents = append(agents, card)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"agents": agents,
		"total":  len(agents),
	})
}

// handleAgentRoutes routes to a2a-go native handlers.
func (s *HTTPServer) handleAgentRoutes(w http.ResponseWriter, r *http.Request) {
	// Parse: /agents/{name}[/...]
	path := strings.TrimPrefix(r.URL.Path, "/agents/")
	if path == "" {
		http.Error(w, "Agent name required", http.StatusBadRequest)
		return
	}

	// Extract agent name and subpath
	parts := strings.SplitN(path, "/", 2)
	agentName := parts[0]
	subPath := ""
	if len(parts) > 1 {
		subPath = "/" + parts[1]
	}

	s.mu.RLock()

	// Verify agent exists
	jsonRPCHandler, ok := s.agentJSONRPCHandlers[agentName]
	if !ok {
		s.mu.RUnlock()
		http.Error(w, "Agent not found: "+agentName, http.StatusNotFound)
		return
	}

	// Check Access Control based on Visibility
	cfg, ok := s.appCfg.Agents[agentName]
	if ok {
		switch cfg.Visibility {
		case "private":
			// Private agents are hidden from HTTP entirely.
			// Treat as 404 to avoid leaking existence.
			s.mu.RUnlock()
			http.NotFound(w, r)
			return
		case "internal":
			// Internal agents require authentication
			if s.authValidator != nil {
				authHeader := r.Header.Get("Authorization")
				authorized := false
				if authHeader != "" {
					token := strings.TrimPrefix(authHeader, "Bearer ")
					if _, err := s.authValidator.ValidateToken(r.Context(), token); err == nil {
						authorized = true
					}
				}
				if !authorized {
					s.mu.RUnlock()
					http.Error(w, "Unauthorized: agent is internal", http.StatusUnauthorized)
					return
				}
			}
		case "public", "":
			// Public access allowed (no check needed)
		}
	}

	cardHandler := s.agentCardHandlers[agentName]
	s.mu.RUnlock()

	switch {
	case subPath == "" || subPath == "/":
		// POST: JSON-RPC (a2a-go native handler)
		// GET: Agent card (a2a-go native handler)
		if r.Method == http.MethodPost {
			jsonRPCHandler.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodOptions {
			cardHandler.ServeHTTP(w, r)
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

	case subPath == a2asrv.WellKnownAgentCardPath:
		// A2A spec: /.well-known/agent-card.json (a2a-go native handler)
		cardHandler.ServeHTTP(w, r)

	default:
		http.NotFound(w, r)
	}
}

// corsMiddleware adds CORS headers.
func (s *HTTPServer) corsMiddleware(next http.Handler) http.Handler {
	cors := s.serverCfg.CORS
	if cors == nil {
		// Default permissive CORS for development
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			for _, allowed := range cors.AllowedOrigins {
				if allowed == "*" || allowed == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if config.BoolValue(cors.AllowCredentials, false) {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs requests (ADK-Go pattern: don't wrap ResponseWriter).
func (s *HTTPServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Don't wrap ResponseWriter - it breaks http.Flusher for SSE
		next.ServeHTTP(w, r)
		slog.Debug("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration", time.Since(start),
		)
	})
}

// UpdateState atomically updates configuration, agent executors, and task store (for hot-reload).
func (s *HTTPServer) UpdateState(cfg *config.Config, executors map[string]*Executor, taskStore a2asrv.TaskStore, taskService task.Service, validator auth.TokenValidator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.serverCfg = &cfg.Server
	s.appCfg = cfg
	s.taskStore = taskStore
	s.taskService = taskService
	s.authValidator = validator

	// Rebuild handlers
	s.agentJSONRPCHandlers = make(map[string]http.Handler)
	s.agentCardHandlers = make(map[string]http.Handler)
	s.agentCards = make(map[string]*a2a.AgentCard)
	s.agentGRPCHandlers = make(map[string]*a2agrpc.Handler)

	s.buildAgentHandlers(executors)
}

// SetStudioMode enables studio mode with config file path.
func (s *HTTPServer) SetStudioMode(configPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.studioMode = true
	s.configPath = configPath
	slog.Info("Studio mode enabled", "config_path", configPath)
}

// SetReloadFunc sets the function to call for synchronous config reload.
// This should trigger the same reload logic as the file watcher,
// returning nil on success or an error if reload fails.
func (s *HTTPServer) SetReloadFunc(fn func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloadFunc = fn
}

// handleConfigEndpoint handles GET and POST /api/config (studio mode).
// Access is controlled by:
//  1. Studio mode must be enabled (--studio flag or server.studio.enabled)
//  2. If auth is enabled, user must have one of the allowed roles
func (s *HTTPServer) handleConfigEndpoint(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	studioMode := s.studioMode
	configPath := s.configPath
	s.mu.RUnlock()

	// Check: Studio mode enabled (via flag or config)
	studioConfigEnabled := s.serverCfg.Studio != nil && s.serverCfg.Studio.IsEnabled()
	if !studioMode && !studioConfigEnabled {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "Studio mode not enabled. Enable via config (server.studio.enabled: true) or --studio flag.",
		})
		return
	}

	// Check: Role-based access when auth is enabled
	if s.serverCfg.Auth != nil && s.serverCfg.Auth.IsEnabled() {
		claims := auth.ClaimsFromContext(r.Context())
		if claims == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Authentication required for studio access.",
			})
			return
		}

		// Get allowed roles (defaults to ["operator"] if not configured)
		allowedRoles := []string{"operator"}
		if s.serverCfg.Studio != nil {
			allowedRoles = s.serverCfg.Studio.GetAllowedRoles()
		}

		if !claims.HasAnyRole(allowedRoles...) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":         "Insufficient permissions. Required role: " + strings.Join(allowedRoles, " or "),
				"current_role":  claims.Role,
				"allowed_roles": strings.Join(allowedRoles, ", "),
			})
			return
		}
	}

	// Use config path from StudioConfig if not set by flag
	if configPath == "" && s.serverCfg.Studio != nil && s.serverCfg.Studio.ConfigPath != "" {
		configPath = s.serverCfg.Studio.ConfigPath
	}

	switch r.Method {
	case http.MethodGet:
		// Try to read config file first
		data, err := os.ReadFile(configPath)
		if err != nil {
			// File doesn't exist (zero-config mode) - use secure template
			if os.IsNotExist(err) {
				// SECURITY: Use template with env var placeholders instead of serializing
				// runtime config which would expose expanded secrets (API keys, etc.)
				template := config.StudioConfigTemplate()
				data = []byte(template)
			} else {
				// Other error reading file
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "Failed to read config file: " + err.Error(),
				})
				return
			}
		}

		// SECURITY: Strip server block before returning to Studio
		// This prevents Studio from accidentally sending it back (which would be rejected)
		data = stripServerBlockFromYAML(data)

		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)

	case http.MethodPost:
		// Read YAML body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Failed to read body: " + err.Error(),
			})
			return
		}

		// Parse and validate
		var testCfg config.Config
		if err := yaml.Unmarshal(body, &testCfg); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Invalid YAML: " + err.Error(),
			})
			return
		}

		// SECURITY: Prevent privilege escalation by blocking server config changes
		// The server block (auth, studio, ports, etc.) is immutable via studio API
		if testCfg.Server.Auth != nil || testCfg.Server.Studio != nil ||
			testCfg.Server.Host != "" || testCfg.Server.Port != 0 ||
			testCfg.Server.TLS != nil || testCfg.Server.CORS != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "Security: server configuration is immutable via studio API",
				"details": "Remove the 'server' block from your configuration. Server settings (auth, studio, ports, TLS, CORS) can only be changed by editing the config file directly.",
			})
			return
		}

		// Preserve existing server config (don't let submitted config override it)
		testCfg.Server = *s.serverCfg

		// Apply defaults before validation to handle missing optional fields
		testCfg.SetDefaults()

		if err := testCfg.Validate(); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Validation failed: " + err.Error(),
			})
			return
		}

		// ATOMIC DEPLOY: Backup old → write new → reload → restore backup if failed
		// This ensures a bad config never persists on disk if reload fails
		s.mu.RLock()
		reloadFn := s.reloadFunc
		s.mu.RUnlock()

		if reloadFn != nil {
			// Atomic deploy with synchronous reload
			backupPath := configPath + ".bak"

			// Step 1: Read existing config for backup (may not exist)
			oldConfig, existsErr := os.ReadFile(configPath)
			configExists := existsErr == nil

			// Step 2: If config exists, create backup
			if configExists {
				if err := os.WriteFile(backupPath, oldConfig, 0644); err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]string{
						"error": "Failed to backup config: " + err.Error(),
					})
					return
				}
			}

			// Step 3: Write new config
			if err := os.WriteFile(configPath, body, 0644); err != nil {
				// Restore backup if we had one
				if configExists {
					_ = os.WriteFile(configPath, oldConfig, 0644)
					_ = os.Remove(backupPath)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "Failed to write config: " + err.Error(),
				})
				return
			}

			// Step 4: Try to reload with the new config
			if err := reloadFn(); err != nil {
				// Reload failed - restore the old config
				if configExists {
					if restoreErr := os.WriteFile(configPath, oldConfig, 0644); restoreErr != nil {
						slog.Error("Failed to restore backup config", "error", restoreErr)
					} else {
						slog.Info("Restored previous config after failed reload")
					}
				}
				_ = os.Remove(backupPath)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"status": "reload_failed",
					"error":  err.Error(),
				})
				return
			}

			// Step 5: Success - clean up backup
			_ = os.Remove(backupPath)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "applied",
				"message": "Configuration saved and applied successfully.",
			})
		} else {
			// No reload function - just write directly (file watcher mode)
			if err := os.WriteFile(configPath, body, 0644); err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "Failed to write file: " + err.Error(),
				})
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status":  "saved",
				"message": "Configuration saved. File watcher will reload automatically.",
			})
		}

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// stripServerBlockFromYAML removes the server: block from YAML config.
// This ensures Studio clients don't accidentally send back server config
// (which would be rejected as immutable).
func stripServerBlockFromYAML(data []byte) []byte {
	var cfgMap map[string]any
	if err := yaml.Unmarshal(data, &cfgMap); err != nil {
		// If we can't parse, return as-is (let the error surface elsewhere)
		return data
	}

	// Remove the server key
	delete(cfgMap, "server")

	// Re-serialize
	result, err := yaml.Marshal(cfgMap)
	if err != nil {
		return data
	}
	return result
}
