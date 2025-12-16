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

// Package mcptoolset provides a Toolset implementation for MCP servers.
//
// MCP (Model Context Protocol) allows connecting to external tool servers
// that expose tools via a standardized protocol.
//
// The toolset uses lazy initialization - the MCP connection is only
// established when Tools() is first called.
//
// Transport Support:
//   - stdio: Uses mcp-go library for subprocess communication
//   - sse, streamable-http: Uses Hector's httpclient with retry/backoff
package mcptoolset

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/verikod/hector/pkg/agent"
	"github.com/verikod/hector/pkg/httpclient"
	"github.com/verikod/hector/pkg/tool"
)

const (
	// DefaultSSEResponseTimeout is the default timeout for reading SSE responses
	// Set to 5 minutes to accommodate long-running operations
	DefaultSSEResponseTimeout = 5 * time.Minute
)

// Config configures an MCP toolset.
type Config struct {
	// Name identifies this toolset.
	Name string

	// URL is the MCP server URL (for HTTP transports).
	URL string

	// Transport specifies the MCP transport (sse, streamable-http, stdio).
	Transport string

	// Command for stdio transport.
	Command string

	// Args for stdio transport.
	Args []string

	// Env for stdio transport.
	Env map[string]string

	// Filter limits which tools are exposed.
	Filter []string

	// MaxRetries for HTTP requests (default: 3).
	MaxRetries int

	// SSETimeout for SSE response reading (default: 5m).
	SSETimeout time.Duration
}

// Toolset is an MCP-backed toolset with lazy initialization.
type Toolset struct {
	cfg Config

	mu         sync.Mutex
	client     *client.Client     // For stdio transport
	httpClient *httpclient.Client // For HTTP transports
	sessionID  string             // For streamable-http transport
	sessionMu  sync.RWMutex
	tools      []tool.Tool
	connected  bool
	filterSet  map[string]bool
}

// New creates a new MCP toolset.
func New(cfg Config) (*Toolset, error) {
	if cfg.URL == "" && cfg.Command == "" {
		return nil, fmt.Errorf("either url or command is required")
	}

	var filterSet map[string]bool
	if len(cfg.Filter) > 0 {
		filterSet = make(map[string]bool, len(cfg.Filter))
		for _, name := range cfg.Filter {
			filterSet[name] = true
		}
	}

	// Set defaults
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.SSETimeout == 0 {
		cfg.SSETimeout = DefaultSSEResponseTimeout
	}

	return &Toolset{
		cfg:       cfg,
		filterSet: filterSet,
	}, nil
}

// Name returns the toolset name.
func (t *Toolset) Name() string {
	return t.cfg.Name
}

// Tools returns the available tools, connecting lazily if needed.
func (t *Toolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Lazy connect
	if !t.connected {
		if err := t.connect(context.Background()); err != nil {
			return nil, fmt.Errorf("failed to connect to MCP server: %w", err)
		}
	}

	return t.tools, nil
}

// WithFilter returns a new toolset that wraps this one with a specific filter.
// The returned toolset shares the underlying connection.
func (t *Toolset) WithFilter(filter []string) tool.Toolset {
	filterSet := make(map[string]bool, len(filter))
	for _, name := range filter {
		filterSet[name] = true
	}

	return &filteredToolset{
		parent:    t,
		filterSet: filterSet,
	}
}

// filteredToolset wraps a Toolset with a strict filter.
type filteredToolset struct {
	parent    *Toolset
	filterSet map[string]bool
}

func (f *filteredToolset) Name() string {
	return f.parent.Name()
}

func (f *filteredToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	tools, err := f.parent.Tools(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []tool.Tool
	for _, t := range tools {
		if f.filterSet[t.Name()] {
			filtered = append(filtered, t)
		}
	}

	return filtered, nil
}

// connect establishes the MCP connection.
func (t *Toolset) connect(ctx context.Context) error {
	// Use different connection strategies based on transport
	if t.cfg.Command != "" || t.cfg.Transport == "stdio" {
		return t.connectStdio(ctx)
	}
	return t.connectHTTP(ctx)
}

// connectStdio connects using mcp-go for subprocess communication.
func (t *Toolset) connectStdio(ctx context.Context) error {
	mcpClient, err := client.NewStdioMCPClient(
		t.cfg.Command,
		t.convertEnv(t.cfg.Env),
		t.cfg.Args...,
	)
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}

	// Start the client
	if err := mcpClient.Start(ctx); err != nil {
		return fmt.Errorf("failed to start MCP client: %w", err)
	}

	// Initialize
	initReq := mcp.InitializeRequest{}
	initReq.Params.ClientInfo = mcp.Implementation{
		Name:    "hector",
		Version: "2.0.0-alpha",
	}
	initReq.Params.ProtocolVersion = "2024-11-05"

	_, err = mcpClient.Initialize(ctx, initReq)
	if err != nil {
		mcpClient.Close()
		return fmt.Errorf("failed to initialize MCP: %w", err)
	}

	// List tools
	listReq := mcp.ListToolsRequest{}
	listResp, err := mcpClient.ListTools(ctx, listReq)
	if err != nil {
		mcpClient.Close()
		return fmt.Errorf("failed to list tools: %w", err)
	}

	// Convert to tool.Tool
	var tools []tool.Tool
	for _, mcpTool := range listResp.Tools {
		// Apply filter
		if t.filterSet != nil && !t.filterSet[mcpTool.Name] {
			continue
		}

		tools = append(tools, &mcpToolWrapper{
			toolset:  t,
			name:     mcpTool.Name,
			desc:     mcpTool.Description,
			schema:   convertSchema(mcpTool.InputSchema),
			useStdio: true,
		})
	}

	t.client = mcpClient
	t.tools = tools
	t.connected = true

	slog.Info("Connected to MCP server (stdio)",
		"name", t.cfg.Name,
		"command", t.cfg.Command,
		"tools", len(tools),
	)

	return nil
}

// connectHTTP connects using Hector's httpclient for HTTP transports.
func (t *Toolset) connectHTTP(ctx context.Context) error {
	// Create HTTP client with retry/backoff
	t.httpClient = httpclient.New(
		httpclient.WithHTTPClient(&http.Client{Timeout: 30 * time.Second}),
		httpclient.WithMaxRetries(t.cfg.MaxRetries),
		httpclient.WithBaseDelay(2*time.Second),
	)

	// Initialize MCP connection
	initResp, err := t.makeHTTPRequest(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo": map[string]any{
			"name":    "hector",
			"version": "2.0.0-alpha",
		},
		"capabilities": map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("failed to initialize MCP: %w", err)
	}

	if initResp.Error != nil {
		return fmt.Errorf("MCP init error: %s", initResp.Error.Message)
	}

	// List tools
	listResp, err := t.makeHTTPRequest(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	if listResp.Error != nil {
		return fmt.Errorf("MCP list error: %s", listResp.Error.Message)
	}

	// Parse tools from result
	resultMap, ok := listResp.Result.(map[string]any)
	if !ok {
		return fmt.Errorf("unexpected result type from tools/list")
	}

	toolsList, ok := resultMap["tools"].([]any)
	if !ok {
		return fmt.Errorf("missing tools in tools/list response")
	}

	// Convert to tool.Tool
	var tools []tool.Tool
	for _, toolRaw := range toolsList {
		toolMap, ok := toolRaw.(map[string]any)
		if !ok {
			continue
		}

		name, _ := toolMap["name"].(string)
		desc, _ := toolMap["description"].(string)

		// Apply filter
		if t.filterSet != nil && !t.filterSet[name] {
			continue
		}

		// Extract input schema
		var schema map[string]any
		if inputSchema, ok := toolMap["inputSchema"].(map[string]any); ok {
			schema = inputSchema
		}

		tools = append(tools, &mcpToolWrapper{
			toolset:  t,
			name:     name,
			desc:     desc,
			schema:   schema,
			useStdio: false,
		})
	}

	t.tools = tools
	t.connected = true

	slog.Info("Connected to MCP server (HTTP)",
		"name", t.cfg.Name,
		"url", t.cfg.URL,
		"transport", t.cfg.Transport,
		"tools", len(tools),
	)

	return nil
}

// JSON-RPC types
type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int           `json:"id"`
	Result  any           `json:"result,omitempty"`
	Error   *jsonRPCError `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// makeHTTPRequest sends a JSON-RPC request over HTTP.
// Uses Hector's httpclient with retry/backoff for rate limit handling.
func (t *Toolset) makeHTTPRequest(ctx context.Context, method string, params any) (*jsonRPCResponse, error) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.cfg.URL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")

	// Add session ID if we have one (for streamable-http transport)
	t.sessionMu.RLock()
	sessionID := t.sessionID
	t.sessionMu.RUnlock()
	if sessionID != "" {
		httpReq.Header.Set("mcp-session-id", sessionID)
	}

	// Use Hector's httpclient with retry/backoff
	httpResp, err := t.httpClient.Do(httpReq)
	if err != nil {
		slog.Debug("MCP HTTP request failed",
			"source", t.cfg.Name,
			"url", t.cfg.URL,
			"method", method,
			"error", err.Error())
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	slog.Debug("MCP HTTP request completed",
		"source", t.cfg.Name,
		"url", t.cfg.URL,
		"method", method,
		"status_code", httpResp.StatusCode,
		"content_type", httpResp.Header.Get("Content-Type"))

	// Extract session ID from response header (for streamable-http transport)
	if newSessionID := httpResp.Header.Get("mcp-session-id"); newSessionID != "" {
		t.sessionMu.Lock()
		t.sessionID = newSessionID
		t.sessionMu.Unlock()
	}

	if httpResp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s (response: %s)", httpResp.StatusCode, httpResp.Status, string(responseBody))
	}

	// Check if response is SSE (Server-Sent Events)
	contentType := httpResp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return t.readSSEResponse(httpResp)
	}

	// Regular JSON response
	responseBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// readSSEResponse reads the first complete JSON-RPC response from an SSE stream.
func (t *Toolset) readSSEResponse(httpResp *http.Response) (*jsonRPCResponse, error) {
	type result struct {
		response *jsonRPCResponse
		err      error
	}
	resultChan := make(chan result, 1)

	go func() {
		defer httpResp.Body.Close()

		reader := bufio.NewReader(httpResp.Body)
		var currentData strings.Builder

		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				slog.Debug("MCP SSE read error", "source", t.cfg.Name, "error", err)
				break
			}

			lineStr := strings.TrimSpace(string(line))

			// Empty line signals end of event
			if lineStr == "" {
				if currentData.Len() > 0 {
					jsonData := currentData.String()
					var resp jsonRPCResponse
					if parseErr := json.Unmarshal([]byte(jsonData), &resp); parseErr == nil {
						resultChan <- result{response: &resp}
						return
					}
					currentData.Reset()
				}
				continue
			}

			// Parse SSE data lines
			if strings.HasPrefix(lineStr, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(lineStr, "data:"))
				currentData.WriteString(data)
			}
		}

		// Handle any remaining data
		if currentData.Len() > 0 {
			jsonData := currentData.String()
			var resp jsonRPCResponse
			if parseErr := json.Unmarshal([]byte(jsonData), &resp); parseErr == nil {
				resultChan <- result{response: &resp}
				return
			}
		}

		resultChan <- result{err: fmt.Errorf("SSE stream ended without complete message")}
	}()

	// Wait for result with timeout
	select {
	case res := <-resultChan:
		if res.err != nil {
			return nil, res.err
		}
		return res.response, nil
	case <-time.After(t.cfg.SSETimeout):
		return nil, fmt.Errorf("timeout reading SSE response after %v", t.cfg.SSETimeout)
	}
}

// convertEnv converts map to slice of "KEY=VALUE".
func (t *Toolset) convertEnv(env map[string]string) []string {
	if env == nil {
		return nil
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

// Close closes the MCP connection.
func (t *Toolset) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.client != nil {
		err := t.client.Close()
		t.client = nil
		t.connected = false
		t.tools = nil
		return err
	}
	// HTTP clients don't need explicit close
	t.httpClient = nil
	t.connected = false
	t.tools = nil
	return nil
}

// mcpToolWrapper wraps an MCP tool as tool.CallableTool.
type mcpToolWrapper struct {
	toolset  *Toolset
	name     string
	desc     string
	schema   map[string]any
	useStdio bool
}

func (w *mcpToolWrapper) Name() string {
	return w.name
}

func (w *mcpToolWrapper) Description() string {
	return w.desc
}

func (w *mcpToolWrapper) IsLongRunning() bool {
	return false
}

func (w *mcpToolWrapper) RequiresApproval() bool {
	return false
}

func (w *mcpToolWrapper) Schema() map[string]any {
	return w.schema
}

func (w *mcpToolWrapper) Call(ctx tool.Context, args map[string]any) (map[string]any, error) {
	if w.useStdio {
		return w.callStdio(ctx, args)
	}
	return w.callHTTP(ctx, args)
}

// callStdio executes tool via mcp-go client (for stdio transport).
func (w *mcpToolWrapper) callStdio(ctx tool.Context, args map[string]any) (map[string]any, error) {
	w.toolset.mu.Lock()
	mcpClient := w.toolset.client
	w.toolset.mu.Unlock()

	if mcpClient == nil {
		return nil, fmt.Errorf("MCP client not connected")
	}

	// Build call request
	req := mcp.CallToolRequest{}
	req.Params.Name = w.name
	req.Params.Arguments = args

	// Call the tool
	bgCtx := context.Background()
	if ctx != nil {
		bgCtx = ctx
	}

	resp, err := mcpClient.CallTool(bgCtx, req)
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	return w.parseToolResponse(resp)
}

// callHTTP executes tool via HTTP (for sse/streamable-http transports).
func (w *mcpToolWrapper) callHTTP(ctx tool.Context, args map[string]any) (map[string]any, error) {
	bgCtx := context.Background()
	if ctx != nil {
		bgCtx = ctx
	}

	resp, err := w.toolset.makeHTTPRequest(bgCtx, "tools/call", map[string]any{
		"name":      w.name,
		"arguments": args,
	})
	if err != nil {
		return nil, fmt.Errorf("MCP call failed: %w", err)
	}

	if resp.Error != nil {
		return map[string]any{
			"error": resp.Error.Message,
		}, nil
	}

	// Parse result
	result := make(map[string]any)
	resultMap, ok := resp.Result.(map[string]any)
	if !ok {
		result["result"] = resp.Result
		return result, nil
	}

	// Check for error
	if isError, _ := resultMap["isError"].(bool); isError {
		if content, ok := resultMap["content"].([]any); ok {
			for _, c := range content {
				if cm, ok := c.(map[string]any); ok {
					if text, ok := cm["text"].(string); ok {
						result["error"] = text
						break
					}
				}
			}
		}
		if result["error"] == nil {
			result["error"] = "unknown error"
		}
		return result, nil
	}

	// Collect text content
	if content, ok := resultMap["content"].([]any); ok {
		var texts []string
		for _, c := range content {
			if cm, ok := c.(map[string]any); ok {
				if cm["type"] == "text" {
					if text, ok := cm["text"].(string); ok {
						texts = append(texts, text)
					}
				}
			}
		}
		if len(texts) == 1 {
			result["result"] = texts[0]
		} else if len(texts) > 1 {
			result["results"] = texts
		}
	}

	return result, nil
}

// parseToolResponse parses MCP tool response into a map.
func (w *mcpToolWrapper) parseToolResponse(resp *mcp.CallToolResult) (map[string]any, error) {
	result := make(map[string]any)
	if resp.IsError {
		// Find error text
		for _, content := range resp.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				result["error"] = textContent.Text
				break
			}
		}
		if result["error"] == nil {
			result["error"] = "unknown error"
		}
	} else {
		// Collect text content
		var texts []string
		for _, content := range resp.Content {
			if textContent, ok := content.(mcp.TextContent); ok {
				texts = append(texts, textContent.Text)
			}
		}
		if len(texts) == 1 {
			result["result"] = texts[0]
		} else if len(texts) > 1 {
			result["results"] = texts
		}
	}

	return result, nil
}

// convertSchema converts MCP tool schema to map.
func convertSchema(schema mcp.ToolInputSchema) map[string]any {
	// Marshal and unmarshal to get a clean map
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	return result
}

// Ensure interfaces are implemented
var (
	_ tool.Toolset      = (*Toolset)(nil)
	_ tool.CallableTool = (*mcpToolWrapper)(nil)
)
