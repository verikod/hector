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

package webtool

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kadirpekel/hector/pkg/httpclient"
	"github.com/kadirpekel/hector/pkg/tool"
	"github.com/kadirpekel/hector/pkg/tool/functiontool"
)

// WebRequestArgs defines the parameters for making HTTP requests.
type WebRequestArgs struct {
	URL     string            `json:"url" jsonschema:"required,description=The URL to request"`
	Method  string            `json:"method,omitempty" jsonschema:"description=HTTP method,default=GET,enum=GET,enum=POST,enum=PUT,enum=DELETE,enum=PATCH,enum=HEAD,enum=OPTIONS"`
	Headers map[string]string `json:"headers,omitempty" jsonschema:"description=HTTP headers as key-value pairs"`
	Body    string            `json:"body,omitempty" jsonschema:"description=Request body (for POST PUT PATCH)"`
}

// WebRequestConfig defines configuration for the web_request tool.
type WebRequestConfig struct {
	Timeout         time.Duration
	MaxRetries      int
	MaxRequestSize  int64
	MaxResponseSize int64
	AllowedDomains  []string
	DeniedDomains   []string
	AllowedMethods  []string
	AllowRedirects  bool
	MaxRedirects    int
	UserAgent       string
}

// NewWebRequest creates a new web_request tool using FunctionTool.
func NewWebRequest(cfg *WebRequestConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &WebRequestConfig{
			Timeout:         30 * time.Second,
			MaxRetries:      3,
			MaxRequestSize:  1048576,  // 1MB
			MaxResponseSize: 10485760, // 10MB
			AllowRedirects:  true,
			MaxRedirects:    10,
			UserAgent:       "Hector/2.0",
		}
	}

	// Create HTTP client with config
	httpClientConfig := &http.Client{
		Timeout: cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !cfg.AllowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) >= cfg.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", cfg.MaxRedirects)
			}
			return nil
		},
	}

	hc := httpclient.New(
		httpclient.WithHTTPClient(httpClientConfig),
		httpclient.WithMaxRetries(cfg.MaxRetries),
	)

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "web_request",
			Description: "Perform HTTP requests. Use this to fetch external resources, interact with APIs, or check web endpoints.",
		},
		func(ctx tool.Context, args WebRequestArgs) (map[string]any, error) {
			return webRequestImpl(cfg, hc, args)
		},
		func(args WebRequestArgs) error {
			// Validate URL
			parsedURL, err := url.Parse(args.URL)
			if err != nil {
				return fmt.Errorf("invalid URL: %w", err)
			}

			// Validate domain
			if err := validateDomain(cfg, parsedURL.Host); err != nil {
				return err
			}

			// Validate method
			method := "GET"
			if args.Method != "" {
				method = strings.ToUpper(args.Method)
			}
			if err := validateMethod(cfg, method); err != nil {
				return err
			}

			// Validate body size
			if int64(len(args.Body)) > cfg.MaxRequestSize {
				return fmt.Errorf("request body too large: %d bytes (max: %d)",
					len(args.Body), cfg.MaxRequestSize)
			}

			return nil
		},
	)
}

func webRequestImpl(cfg *WebRequestConfig, hc *httpclient.Client, args WebRequestArgs) (map[string]any, error) {
	// Determine method
	method := "GET"
	if args.Method != "" {
		method = strings.ToUpper(args.Method)
	}

	// Prepare body
	var body io.Reader
	if args.Body != "" {
		body = bytes.NewReader([]byte(args.Body))
	}

	// Create request
	req, err := http.NewRequest(method, args.URL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("User-Agent", cfg.UserAgent)
	for k, v := range args.Headers {
		req.Header.Set(k, v)
	}

	// Execute request
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body with size limit
	limitedReader := io.LimitReader(resp.Body, cfg.MaxResponseSize+1)
	responseBody, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if int64(len(responseBody)) > cfg.MaxResponseSize {
		return nil, fmt.Errorf("response too large: exceeds %d bytes", cfg.MaxResponseSize)
	}

	// Build response headers map
	respHeaders := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			respHeaders[k] = v[0]
		}
	}

	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	return map[string]any{
		"success":      success,
		"content":      string(responseBody),
		"url":          args.URL,
		"method":       method,
		"status_code":  resp.StatusCode,
		"status":       resp.Status,
		"headers":      respHeaders,
		"content_type": resp.Header.Get("Content-Type"),
		"size":         len(responseBody),
	}, nil
}

func validateDomain(cfg *WebRequestConfig, host string) error {
	// If no domain restrictions, allow all
	if len(cfg.AllowedDomains) == 0 && len(cfg.DeniedDomains) == 0 {
		return nil
	}

	// Check denied list first (takes precedence)
	for _, denied := range cfg.DeniedDomains {
		if matchesDomain(host, denied) {
			return fmt.Errorf("domain not allowed: %s (matches deny rule: %s)", host, denied)
		}
	}

	// If allowed list is specified, check it
	if len(cfg.AllowedDomains) > 0 {
		for _, allowed := range cfg.AllowedDomains {
			if matchesDomain(host, allowed) {
				return nil
			}
		}
		return fmt.Errorf("domain not allowed: %s (not in allowed list)", host)
	}

	return nil
}

func validateMethod(cfg *WebRequestConfig, method string) error {
	// If no method restrictions, allow all
	if len(cfg.AllowedMethods) == 0 {
		return nil
	}

	for _, allowed := range cfg.AllowedMethods {
		if strings.EqualFold(method, allowed) {
			return nil
		}
	}

	return fmt.Errorf("HTTP method not allowed: %s (allowed: %v)", method, cfg.AllowedMethods)
}

func matchesDomain(host, pattern string) bool {
	// Remove port if present
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Exact match
	if host == pattern {
		return true
	}

	// Wildcard match (e.g., "*.example.com")
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // Remove '*'
		return strings.HasSuffix(host, suffix)
	}

	return false
}
