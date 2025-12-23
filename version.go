// SPDX-License-Identifier: AGPL-3.0
// Copyright 2025 Kadir Pekel
//
// Package hector provides build-time version information.
// These variables are populated via ldflags during build.
package hector

// Version information set at build time via ldflags.
// When building with make: make build (uses git describe)
// When installing via go install: populated from module version
var (
	// Version is the semantic version (e.g., "v0.1.1").
	// Set via: -X 'github.com/verikod/hector.Version=$(VERSION)'
	Version = "1.12.0"

	// GitCommit is the short git commit hash.
	// Set via: -X 'github.com/verikod/hector.GitCommit=$(GIT_COMMIT)'
	GitCommit = "unknown"

	// BuildDate is the build timestamp in ISO 8601 format.
	// Set via: -X 'github.com/verikod/hector.BuildDate=$(BUILD_DATE)'
	BuildDate = "unknown"
)
