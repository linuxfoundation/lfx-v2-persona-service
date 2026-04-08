// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenFunc returns a bearer token for authenticating against the Query Service.
// When nil, no Authorization header is sent (direct/local access).
type TokenFunc func(ctx context.Context) (string, error)

// Client provides access to the Query Service resource search API.
type Client struct {
	baseURL    string
	tokenFunc  TokenFunc
	httpClient *http.Client
}

// ClientConfig holds configuration for creating a Query Service Client.
type ClientConfig struct {
	// BaseURL is the query service base URL.
	// When using QUERY_SERVICE_URL this is the direct URL.
	// When using LFX_BASE_URL this is the API gateway URL.
	BaseURL    string
	TokenFunc  TokenFunc
	HTTPClient *http.Client
}

// NewClient creates a new Query Service client.
func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		tokenFunc:  cfg.TokenFunc,
		httpClient: httpClient,
	}
}

// SearchParams defines the query parameters for a resource search.
type SearchParams struct {
	Type    string
	TagsAll []string // AND-matched tag values
	Filters []string // term clauses against data fields
}

// Search calls GET /query/resources?v=1 with the given parameters.
func (c *Client) Search(ctx context.Context, params SearchParams) ([]Resource, error) {
	u, err := url.Parse(c.baseURL + "/query/resources")
	if err != nil {
		return nil, fmt.Errorf("parse query URL: %w", err)
	}

	q := u.Query()
	q.Set("v", "1")
	if params.Type != "" {
		q.Set("type", params.Type)
	}
	if len(params.TagsAll) > 0 {
		q.Set("tags_all", strings.Join(params.TagsAll, ","))
	}
	if len(params.Filters) > 0 {
		q.Set("filters", strings.Join(params.Filters, ","))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create query request: %w", err)
	}

	if c.tokenFunc != nil {
		token, tokenErr := c.tokenFunc(ctx)
		if tokenErr != nil {
			return nil, fmt.Errorf("get query service token: %w", tokenErr)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	slog.DebugContext(ctx, "query service search",
		"url", u.String(),
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read query response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "query service search failed",
			"status", resp.StatusCode,
			"body", string(body),
		)
		return nil, fmt.Errorf("query returned %d", resp.StatusCode)
	}

	var result ResourceResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshal query response: %w", err)
	}

	return result.Resources, nil
}
