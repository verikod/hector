// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel

// Package server provides the HTTP and gRPC server implementation for Hector.
//
// The server exposes REST and gRPC APIs for agent management and execution.
// Use the --studio flag to enable Studio Mode API endpoints for remote UI connections.
//
// To build the server:
//
//	go build ./cmd/hector
//
// Or use the Makefile targets:
//
//	make build        # Development build
//	make build-release # Production build
//	make install      # Install to GOPATH/bin
package server
