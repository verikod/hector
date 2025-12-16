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
	"fmt"
	"net/http"

	"github.com/a2aproject/a2a-go/a2asrv"

	"github.com/verikod/hector/pkg/runner"
	"github.com/verikod/hector/pkg/server"
)

// ServerBuilder provides a fluent API for building A2A servers.
//
// Servers expose agents via the A2A (Agent-to-Agent) protocol,
// supporting JSON-RPC, gRPC, and HTTP transports.
//
// Example:
//
//	srv, err := builder.NewServer().
//	    WithRunner(myRunner).
//	    Address(":8080").
//	    Build()
type ServerBuilder struct {
	runner          *runner.Runner
	address         string
	enableUI        bool
	enableStreaming bool
}

// NewServer creates a new server builder.
//
// Example:
//
//	srv, err := builder.NewServer().
//	    WithRunner(r).
//	    Address(":8080").
//	    Build()
func NewServer() *ServerBuilder {
	return &ServerBuilder{
		address:         ":8080",
		enableStreaming: true,
	}
}

// WithRunner sets the runner for the server.
//
// Example:
//
//	builder.NewServer().WithRunner(myRunner)
func (b *ServerBuilder) WithRunner(r *runner.Runner) *ServerBuilder {
	if r == nil {
		panic("runner cannot be nil")
	}
	b.runner = r
	return b
}

// Address sets the server listen address.
//
// Example:
//
//	builder.NewServer().Address(":9000")
func (b *ServerBuilder) Address(addr string) *ServerBuilder {
	b.address = addr
	return b
}

// EnableUI enables the built-in web UI.
//
// Example:
//
//	builder.NewServer().EnableUI(true)
func (b *ServerBuilder) EnableUI(enabled bool) *ServerBuilder {
	b.enableUI = enabled
	return b
}

// EnableStreaming enables streaming responses.
//
// Example:
//
//	builder.NewServer().EnableStreaming(true)
func (b *ServerBuilder) EnableStreaming(enabled bool) *ServerBuilder {
	b.enableStreaming = enabled
	return b
}

// Build creates the HTTP handler for the server.
//
// Returns an error if required parameters are missing.
func (b *ServerBuilder) Build() (http.Handler, error) {
	if b.runner == nil {
		return nil, fmt.Errorf("runner is required: use WithRunner()")
	}

	// Create executor
	executor := server.NewExecutor(server.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        b.runner.AppName(),
			Agent:          b.runner.RootAgent(),
			SessionService: nil, // Runner already has this
		},
	})

	// Create A2A handler
	handler := a2asrv.NewHandler(executor)
	httpHandler := a2asrv.NewJSONRPCHandler(handler)

	// Create mux
	mux := http.NewServeMux()
	mux.Handle("/", httpHandler)
	mux.Handle("/a2a", httpHandler)

	// Add UI if enabled
	if b.enableUI {
		// Static files would be served here
		mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServer(http.Dir("ui/dist"))))
	}

	return mux, nil
}

// Server represents a built A2A server.
type Server struct {
	handler http.Handler
	address string
}

// BuildServer creates a complete server ready to serve.
//
// Example:
//
//	srv, _ := builder.NewServer().
//	    WithRunner(r).
//	    BuildServer()
//	srv.ListenAndServe()
func (b *ServerBuilder) BuildServer() (*Server, error) {
	handler, err := b.Build()
	if err != nil {
		return nil, err
	}
	return &Server{
		handler: handler,
		address: b.address,
	}, nil
}

// MustBuildServer creates the server or panics on error.
func (b *ServerBuilder) MustBuildServer() *Server {
	srv, err := b.BuildServer()
	if err != nil {
		panic(fmt.Sprintf("failed to build server: %v", err))
	}
	return srv
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe() error {
	return http.ListenAndServe(s.address, s.handler)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Address returns the server address.
func (s *Server) Address() string {
	return s.address
}
