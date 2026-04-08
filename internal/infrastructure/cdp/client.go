// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client provides access to the CDP API (resolve + project-affiliations).
type Client struct {
	baseURL    string
	tokenProv  *TokenProvider
	httpClient *http.Client
}

// ClientConfig holds the configuration for creating a CDP Client.
type ClientConfig struct {
	BaseURL    string
	TokenProv  *TokenProvider
	HTTPClient *http.Client
}

// NewClient creates a new CDP API client.
func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		tokenProv:  cfg.TokenProv,
		httpClient: httpClient,
	}
}

// ResolveMember calls POST /v1/members/resolve and returns the CDP memberId.
// Returns empty string and nil error when CDP returns 404 (no profile).
func (c *Client) ResolveMember(ctx context.Context, username, email string) (string, error) {
	token, err := c.tokenProv.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("get CDP token: %w", err)
	}

	reqBody := ResolveRequest{}
	if username != "" {
		reqBody.LFIDs = []string{username}
	}
	if email != "" {
		reqBody.Emails = []string{email}
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal resolve request: %w", err)
	}

	resolveURL := c.baseURL + "/v1/members/resolve"
	requestID := uuid.NewString()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolveURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return "", fmt.Errorf("create resolve request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-LFX-Request-ID", requestID)

	slog.DebugContext(ctx, "CDP resolve member",
		"url", resolveURL,
		"request_id", requestID,
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read resolve response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		slog.DebugContext(ctx, "CDP member not found (404)")
		return "", nil
	}

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "CDP resolve failed",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return "", fmt.Errorf("resolve returned %d", resp.StatusCode)
	}

	var resolveResp ResolveResponse
	if err := json.Unmarshal(respBody, &resolveResp); err != nil {
		return "", fmt.Errorf("unmarshal resolve response: %w", err)
	}

	slog.DebugContext(ctx, "CDP member resolved",
		"member_id", resolveResp.MemberID,
	)

	return resolveResp.MemberID, nil
}

// GetProjectAffiliations calls GET /v1/members/{memberId}/project-affiliations.
func (c *Client) GetProjectAffiliations(ctx context.Context, memberID string) ([]ProjectAffiliation, error) {
	token, err := c.tokenProv.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("get CDP token: %w", err)
	}

	affURL := fmt.Sprintf("%s/v1/members/%s/project-affiliations", c.baseURL, memberID)
	requestID := uuid.NewString()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, affURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create affiliations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-LFX-Request-ID", requestID)

	slog.DebugContext(ctx, "CDP get project affiliations",
		"url", affURL,
		"member_id", memberID,
		"request_id", requestID,
	)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("affiliations request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read affiliations response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		slog.ErrorContext(ctx, "CDP affiliations failed",
			"status", resp.StatusCode,
			"body", string(respBody),
		)
		return nil, fmt.Errorf("affiliations returned %d", resp.StatusCode)
	}

	var affResp ProjectAffiliationsResponse
	if err := json.Unmarshal(respBody, &affResp); err != nil {
		return nil, fmt.Errorf("unmarshal affiliations response: %w", err)
	}

	slog.DebugContext(ctx, "CDP affiliations fetched",
		"member_id", memberID,
		"count", len(affResp.ProjectAffiliations),
	)

	return affResp.ProjectAffiliations, nil
}
