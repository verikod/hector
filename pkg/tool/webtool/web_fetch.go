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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/verikod/hector/pkg/httpclient"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// WebFetchArgs defines the parameters for the standard web_fetch tool.
// Schema follows Anthropic's Server Tool definition.
type WebFetchArgs struct {
	URL string `json:"url" jsonschema:"required,description=The URL to fetch content from"`
}

// WebFetchConfig defines configuration for the web_fetch tool.
type WebFetchConfig struct {
	Timeout         time.Duration
	MaxRetries      int
	MaxResponseSize int64
	AllowedDomains  []string
	DeniedDomains   []string
	AllowRedirects  bool
	UserAgent       string
}

// NewWebFetch creates a new web_fetch tool using FunctionTool.
func NewWebFetch(cfg *WebFetchConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &WebFetchConfig{
			Timeout:         30 * time.Second,
			MaxRetries:      3,
			MaxResponseSize: 10485760, // 10MB
			AllowRedirects:  true,
			UserAgent:       "Hector/2.0 (web_fetch)",
		}
	}

	// Create HTTP client using shared package logic
	// We duplicate the checking logic here to keep tools independent but consistent
	httpClientConfig := &http.Client{
		Timeout: cfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !cfg.AllowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
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
			Name:        "web_fetch",
			Description: "Fetch the content of a URL. Returns the Page Content. Use this to read documentation, news, or any public web page.",
		},
		func(ctx tool.Context, args WebFetchArgs) (map[string]any, error) {
			return webFetchImpl(cfg, hc, args)
		},
		func(args WebFetchArgs) error {
			parsedURL, err := url.Parse(args.URL)
			if err != nil {
				return fmt.Errorf("invalid URL: %w", err)
			}
			// Basic scheme validation
			if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
				return fmt.Errorf("only http and https schemes are supported")
			}
			return nil
		},
	)
}

func webFetchImpl(cfg *WebFetchConfig, hc *httpclient.Client, args WebFetchArgs) (map[string]any, error) {
	req, err := http.NewRequest("GET", args.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", cfg.UserAgent)

	// Execute request
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response
	limitedReader := io.LimitReader(resp.Body, cfg.MaxResponseSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if int64(len(body)) > cfg.MaxResponseSize {
		return nil, fmt.Errorf("content too large: exceeds %d bytes", cfg.MaxResponseSize)
	}

	// TODO: Integrate HTML-to-Markdown conversion (e.g., using a library like github.com/JohannesKaufmann/html-to-markdown)
	// For now, we return the raw source or basic text.
	// Anthropic's web_fetch often returns 'page_content' which is stripped/cleaned.
	// We'll return it as 'content' string.

	return map[string]any{
		"content":      string(body), // Raw HTML for now, model can parse or we add converter later
		"url":          args.URL,
		"status_code":  resp.StatusCode,
		"content_type": resp.Header.Get("Content-Type"),
	}, nil
}
