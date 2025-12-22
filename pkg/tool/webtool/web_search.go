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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/verikod/hector/pkg/httpclient"
	"github.com/verikod/hector/pkg/tool"
	"github.com/verikod/hector/pkg/tool/functiontool"
)

// WebSearchArgs defines the parameters for the standard web_search tool.
type WebSearchArgs struct {
	Query string `json:"query" jsonschema:"required,description=The search query"`
}

// WebSearchConfig configures the tool.
type WebSearchConfig struct {
	Provider string // tavily, serpapi, google (default: tavily)
	APIKey   string // If empty, loaded from env based on provider
	Timeout  time.Duration
}

// SearchResult represents a single search result.
type SearchResult struct {
	Title   string  `json:"title"`
	URL     string  `json:"url"`
	Content string  `json:"content"` // Snippet or full content depending on provider
	Score   float64 `json:"score,omitempty"`
}

// SearchProvider defines the interface for search backends.
type SearchProvider interface {
	Search(ctx tool.Context, query string) ([]SearchResult, error)
}

// NewWebSearch creates a new web_search tool.
func NewWebSearch(cfg *WebSearchConfig) (tool.CallableTool, error) {
	if cfg == nil {
		cfg = &WebSearchConfig{
			Provider: "tavily",
			Timeout:  30 * time.Second,
		}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}

	// Initialize provider
	var provider SearchProvider

	switch cfg.Provider {
	case "tavily":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("TAVILY_API_KEY")
		}
		// If apiKey is empty, the tool will fail at runtime (TavilyProvider checks it).
		// We allow creation without key to support checking requirements later or mocking.
		provider = NewTavilyProvider(apiKey, cfg.Timeout)
	default:
		// Default to Tavily if unknown, or error?
		// Fallback to Tavily context
		provider = NewTavilyProvider(os.Getenv("TAVILY_API_KEY"), cfg.Timeout)
	}

	return functiontool.NewWithValidation(
		functiontool.Config{
			Name:        "web_search",
			Description: "Search the internet for information. Returns relevant results with summaries. Use this to find up-to-date information, news, or answers to questions not in your training data.",
		},
		func(ctx tool.Context, args WebSearchArgs) (map[string]any, error) {
			results, err := provider.Search(ctx, args.Query)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"results":  results,
				"count":    len(results),
				"provider": cfg.Provider,
			}, nil
		},
		func(args WebSearchArgs) error {
			if args.Query == "" {
				return fmt.Errorf("query is required")
			}
			return nil
		},
	)
}

// --- Tavily Provider Implementation ---

type TavilyProvider struct {
	apiKey string
	client *httpclient.Client
}

func NewTavilyProvider(apiKey string, timeout time.Duration) *TavilyProvider {
	return &TavilyProvider{
		apiKey: apiKey,
		client: httpclient.New(httpclient.WithHTTPClient(&http.Client{Timeout: timeout})),
	}
}

type tavilyRequest struct {
	APIKey            string `json:"api_key"`
	Query             string `json:"query"`
	SearchDepth       string `json:"search_depth"` // "basic" or "advanced"
	IncludeAnswer     bool   `json:"include_answer"`
	IncludeRawContent bool   `json:"include_raw_content"`
	MaxResults        int    `json:"max_results"`
}

type tavilyResponse struct {
	Answer  string `json:"answer"`
	Results []struct {
		Title   string  `json:"title"`
		URL     string  `json:"url"`
		Content string  `json:"content"`
		Score   float64 `json:"score"`
	} `json:"results"`
}

func (p *TavilyProvider) Search(ctx tool.Context, query string) ([]SearchResult, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY is not set")
	}

	reqBody := tavilyRequest{
		APIKey:      p.apiKey,
		Query:       query,
		SearchDepth: "basic", // Default to basic for speed
		MaxResults:  5,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", "https://api.tavily.com/search", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tavily search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("tavily API error: %s", resp.Status)
	}

	body, _ := io.ReadAll(resp.Body)
	var tResp tavilyResponse
	if err := json.Unmarshal(body, &tResp); err != nil {
		return nil, fmt.Errorf("failed to parse tavily response: %w", err)
	}

	var results []SearchResult
	// If Tavily generates a direct answer, include it as a result
	if tResp.Answer != "" {
		results = append(results, SearchResult{
			Title:   "Direct Answer",
			Content: tResp.Answer,
			Score:   1.0,
		})
	}

	for _, r := range tResp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
			Score:   r.Score,
		})
	}

	return results, nil
}
