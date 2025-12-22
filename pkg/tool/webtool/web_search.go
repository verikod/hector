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
// Designed to be generic across providers while supporting common advanced features.
type WebSearchArgs struct {
	Query string `json:"query" jsonschema:"required,description=The search query"`
	Topic string `json:"topic,omitempty" jsonschema:"enum=general,enum=news,description=The category of the search. Use 'news' for recent events."`
	// TimeRange filters results by date. Supported values: day, week, month, year.
	TimeRange string `json:"time_range,omitempty" jsonschema:"enum=day,enum=week,enum=month,enum=year,description=Time range for search results (day, week, month, year)."`
	// MaxResults limits the number of results returned (default: 5).
	MaxResults int `json:"max_results,omitempty" jsonschema:"description=Maximum number of results to return"`
	// IncludeDomains list of domains to specifically include.
	IncludeDomains []string `json:"include_domains,omitempty" jsonschema:"description=Domains to include in search"`
	// ExcludeDomains list of domains to specifically exclude.
	ExcludeDomains []string `json:"exclude_domains,omitempty" jsonschema:"description=Domains to exclude from search"`
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
	Content string  `json:"content"`
	Score   float64 `json:"score,omitempty"`
}

// SearchResponse encapsulates the results and metadata.
type SearchResponse struct {
	Results []SearchResult `json:"results"`
	Answer  string         `json:"answer,omitempty"` // Direct answer if supported by provider
}

// SearchProvider defines the interface for search backends.
type SearchProvider interface {
	Search(ctx tool.Context, args WebSearchArgs) (*SearchResponse, error)
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
			response, err := provider.Search(ctx, args)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"results":  response.Results,
				"answer":   response.Answer,
				"count":    len(response.Results),
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

// tavilyRequest matches the official Tavily OpenAPI /search spec.
type tavilyRequest struct {
	APIKey            string   `json:"api_key"`
	Query             string   `json:"query"`
	Topic             string   `json:"topic,omitempty"`        // "general", "news"
	SearchDepth       string   `json:"search_depth,omitempty"` // "basic", "advanced"
	MaxResults        int      `json:"max_results,omitempty"`
	IncludeAnswer     bool     `json:"include_answer,omitempty"`
	IncludeRawContent bool     `json:"include_raw_content,omitempty"`
	IncludeImages     bool     `json:"include_images,omitempty"`
	IncludeDomains    []string `json:"include_domains,omitempty"`
	ExcludeDomains    []string `json:"exclude_domains,omitempty"`
	TimeRange         string   `json:"time_range,omitempty"` // "day", "week", "month", "year"
}

type tavilyResponse struct {
	Answer  string `json:"answer"`
	Results []struct {
		Title      string  `json:"title"`
		URL        string  `json:"url"`
		Content    string  `json:"content"`
		Score      float64 `json:"score"`
		RawContent string  `json:"raw_content,omitempty"`
	} `json:"results"`
}

func (p *TavilyProvider) Search(ctx tool.Context, args WebSearchArgs) (*SearchResponse, error) {
	if p.apiKey == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY is not set")
	}

	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20 // Tavily max
	}

	// Map generic args to Tavily specific request
	reqBody := tavilyRequest{
		APIKey:            p.apiKey,
		Query:             args.Query,
		SearchDepth:       "basic", // Default to basic
		MaxResults:        maxResults,
		IncludeAnswer:     true,  // Always include answer for agents
		IncludeRawContent: false, // Keep false to avoid token overload
		Topic:             "general",
		TimeRange:         args.TimeRange,
		IncludeDomains:    args.IncludeDomains,
		ExcludeDomains:    args.ExcludeDomains,
	}

	if args.Topic == "news" {
		reqBody.Topic = "news"
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
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tavily API error %s: %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read tavily response: %w", err)
	}

	var tResp tavilyResponse
	if err := json.Unmarshal(body, &tResp); err != nil {
		return nil, fmt.Errorf("failed to parse tavily response: %w", err)
	}

	var results []SearchResult
	for _, r := range tResp.Results {
		results = append(results, SearchResult{
			Title:   r.Title,
			URL:     r.URL,
			Content: r.Content,
			Score:   r.Score,
		})
	}

	return &SearchResponse{
		Results: results,
		Answer:  tResp.Answer,
	}, nil
}
