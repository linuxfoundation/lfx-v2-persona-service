// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/cdp"
	natsclient "github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/nats"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
)

// personaHandler implements port.MessageHandler.
type personaHandler struct {
	cdpClient   *cdp.Client
	cdpCache    *cdp.Cache
	queryClient *query.Client
	natsClient  *natsclient.NATSClient
}

// PersonaHandlerOption configures the personaHandler.
type PersonaHandlerOption func(*personaHandler)

// WithCDP enables the CDP source (Source 2: cdp_roles).
func WithCDP(client *cdp.Client, cache *cdp.Cache) PersonaHandlerOption {
	return func(h *personaHandler) {
		h.cdpClient = client
		h.cdpCache = cache
	}
}

// WithQueryService enables Query Service-based sources (Board Member, Source 4, etc.).
func WithQueryService(client *query.Client, nc *natsclient.NATSClient) PersonaHandlerOption {
	return func(h *personaHandler) {
		h.queryClient = client
		h.natsClient = nc
	}
}

// GetPersona validates the request and fans out to enabled sources.
func (h *personaHandler) GetPersona(ctx context.Context, msg port.TransportMessenger) ([]byte, error) {

	var req model.PersonaRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		slog.ErrorContext(ctx, "failed to unmarshal persona request", "error", err)
		return errorResponse("invalid_request", "malformed JSON payload")
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Username = strings.TrimSpace(req.Username)

	if req.Email == "" {
		return errorResponse("validation_error", "email is required")
	}

	slog.DebugContext(ctx, "persona request received",
		"username", req.Username,
		"email", req.Email,
	)

	// Resolve username → Auth0 sub once (shared by Query Service sources).
	var sub string
	if h.queryClient != nil && req.Username != "" {
		var subErr error
		sub, subErr = h.resolveUsernameToSub(ctx, req.Username)
		if subErr != nil {
			slog.WarnContext(ctx, "username→sub resolution failed", "error", subErr)
		}
	}

	// Fan out to enabled sources in parallel.
	type sourceResult struct {
		projects []model.Project
		err      error
		name     string
	}

	var wg sync.WaitGroup
	results := make(chan sourceResult, 8)

	// Board Member + Source 4 (committee membership).
	if h.queryClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := h.sourceBoardMemberAndCommittee(ctx, &req, sub)
			results <- sourceResult{p, err, "board_member+committee"}
		}()
	}

	// Executive Director.
	if h.queryClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := h.sourceExecutiveDirector(ctx, &req, sub)
			results <- sourceResult{p, err, "executive_director"}
		}()
	}

	// Source 2: CDP roles and affiliations.
	if h.cdpClient != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := h.sourceCDPRoles(ctx, &req)
			results <- sourceResult{p, err, "cdp_roles"}
		}()
	}

	// Close results channel when all sources finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	var projects []model.Project
	for r := range results {
		if r.err != nil {
			slog.ErrorContext(ctx, "source failed, skipping",
				"source", r.name,
				"error", r.err,
			)
			continue
		}
		projects = model.MergeProjects(projects, r.projects)
	}

	resp := model.PersonaResponse{
		Projects: projects,
		Error:    nil,
	}
	// Ensure projects is never null in JSON.
	if resp.Projects == nil {
		resp.Projects = []model.Project{}
	}

	return json.Marshal(resp)
}

// sourceCDPRoles implements the two-step CDP flow: resolve → affiliations.
// Results are cached in NATS KV with stale-while-revalidate.
func (h *personaHandler) sourceCDPRoles(ctx context.Context, req *model.PersonaRequest) ([]model.Project, error) {

	// Step 1: Resolve memberId (check cache first).
	memberID, cacheResult, err := h.cdpCache.GetMemberID(ctx, req.Username)
	if err != nil {
		slog.WarnContext(ctx, "cache lookup member_id failed", "error", err)
	}

	if !cacheResult.Hit {
		// Cache miss — fetch synchronously.
		memberID, err = h.cdpClient.ResolveMember(ctx, req.Username, req.Email)
		if err != nil {
			return nil, err
		}
		if memberID == "" {
			// No CDP profile for this user.
			return nil, nil
		}
		h.cdpCache.PutMemberID(ctx, req.Username, memberID)
	} else if cacheResult.Stale {
		// Stale — return cached value, refresh in background.
		go h.backgroundRefreshMemberID(req.Username, req.Email)
	}

	if memberID == "" {
		return nil, nil
	}

	// Step 2: Fetch project affiliations (check cache first).
	affiliations, affCache, err := h.cdpCache.GetAffiliations(ctx, memberID)
	if err != nil {
		slog.WarnContext(ctx, "cache lookup affiliations failed", "error", err)
	}

	if !affCache.Hit {
		affiliations, err = h.cdpClient.GetProjectAffiliations(ctx, memberID)
		if err != nil {
			return nil, err
		}
		h.cdpCache.PutAffiliations(ctx, memberID, affiliations)
	} else if affCache.Stale {
		go h.backgroundRefreshAffiliations(memberID)
	}

	// Step 3: Resolve CDP slugs → v2 project UIDs, then convert to detections.
	slugMap := h.resolveSlugsToUIDs(ctx, affiliations)
	return affiliationsToProjects(affiliations, slugMap)
}

const projectServiceSlugToUID = "lfx.project-service.slug_to_uid"

// resolveSlugsToUIDs resolves CDP project slugs to v2 project UIDs in parallel
// via the project service NATS endpoint. Returns a map of slug → v2 UID.
func (h *personaHandler) resolveSlugsToUIDs(ctx context.Context, affiliations []cdp.ProjectAffiliation) map[string]string {
	if h.natsClient == nil {
		return nil
	}

	// Collect unique slugs, skipping CDP-only "nonlf_" projects.
	unique := make(map[string]bool)
	for _, aff := range affiliations {
		if aff.ProjectSlug != "" && !strings.HasPrefix(aff.ProjectSlug, "nonlf_") {
			unique[aff.ProjectSlug] = true
		}
	}

	if len(unique) == 0 {
		return nil
	}

	type result struct {
		slug string
		uid  string
	}

	var wg sync.WaitGroup
	ch := make(chan result, len(unique))

	for slug := range unique {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			resp, err := h.natsClient.Request(ctx, projectServiceSlugToUID, []byte(s))
			if err != nil {
				slog.WarnContext(ctx, "slug→uid resolution failed", "slug", s, "error", err)
				return
			}
			uid := strings.TrimSpace(string(resp))
			if uid == "" || (len(resp) > 0 && resp[0] == '{') {
				slog.WarnContext(ctx, "slug→uid returned empty or error", "slug", s, "response", string(resp))
				return
			}
			ch <- result{slug: s, uid: uid}
		}(slug)
	}

	wg.Wait()
	close(ch)

	out := make(map[string]string, len(unique))
	for r := range ch {
		out[r.slug] = r.uid
	}

	slog.DebugContext(ctx, "resolved CDP slugs to v2 UIDs", "requested", len(unique), "resolved", len(out))

	return out
}

func affiliationsToProjects(affiliations []cdp.ProjectAffiliation, slugMap map[string]string) ([]model.Project, error) {
	projects := make([]model.Project, 0, len(affiliations))

	for _, aff := range affiliations {
		roles := make([]model.CDPRolesExtraRole, 0, len(aff.Roles))
		for _, r := range aff.Roles {
			roles = append(roles, model.CDPRolesExtraRole{
				ID:          r.ID,
				Role:        r.Role,
				StartDate:   r.StartDate,
				EndDate:     r.EndDate,
				RepoURL:     r.RepoURL,
				RepoFileURL: r.RepoFileURL,
			})
		}

		extra := model.CDPRolesExtra{
			ContributionCount: aff.ContributionCount,
			Roles:             roles,
		}
		extraJSON, err := json.Marshal(extra)
		if err != nil {
			return nil, err
		}

		// Skip nonlf_ slugs and entries that failed v2 UID resolution.
		if strings.HasPrefix(aff.ProjectSlug, "nonlf_") {
			continue
		}
		projectUID, ok := slugMap[aff.ProjectSlug]
		if !ok || projectUID == "" {
			continue
		}

		projects = append(projects, model.Project{
			ProjectUID:  projectUID,
			ProjectSlug: aff.ProjectSlug,
			Detections: []model.Detection{
				{
					Source: model.SourceCDPRoles,
					Extra:  extraJSON,
				},
			},
		})
	}

	return projects, nil
}

// backgroundRefreshMemberID refreshes the cached memberId without blocking.
func (h *personaHandler) backgroundRefreshMemberID(username, email string) {
	ctx := context.Background()
	memberID, err := h.cdpClient.ResolveMember(ctx, username, email)
	if err != nil {
		slog.WarnContext(ctx, "background refresh member_id failed", "error", err)
		return
	}
	if memberID != "" {
		h.cdpCache.PutMemberID(ctx, username, memberID)
	}
}

// backgroundRefreshAffiliations refreshes the cached affiliations without blocking.
func (h *personaHandler) backgroundRefreshAffiliations(memberID string) {
	ctx := context.Background()
	affiliations, err := h.cdpClient.GetProjectAffiliations(ctx, memberID)
	if err != nil {
		slog.WarnContext(ctx, "background refresh affiliations failed", "error", err)
		return
	}
	h.cdpCache.PutAffiliations(ctx, memberID, affiliations)
}

const authServiceUsernameToSub = "lfx.auth-service.username_to_sub"

// resolveUsernameToSub calls the auth service via NATS to translate an LFX
// username into an Auth0 sub (e.g. "auth0|abc123"). Returns empty string if
// the user is not found.
func (h *personaHandler) resolveUsernameToSub(ctx context.Context, username string) (string, error) {
	if h.natsClient == nil {
		return "", fmt.Errorf("NATS client not available for username→sub lookup")
	}

	resp, err := h.natsClient.Request(ctx, authServiceUsernameToSub, []byte(username))
	if err != nil {
		return "", fmt.Errorf("auth service username_to_sub: %w", err)
	}

	// On error the auth service returns JSON like {"success":false,"error":"..."}
	if len(resp) > 0 && resp[0] == '{' {
		slog.WarnContext(ctx, "username→sub lookup returned error response",
			"username", username,
			"response", string(resp),
		)
		return "", nil
	}

	sub := strings.TrimSpace(string(resp))
	slog.DebugContext(ctx, "resolved username to sub",
		"username", username,
		"sub", sub,
	)
	return sub, nil
}

// errorResponse builds a PersonaResponse with an error and empty projects.
func errorResponse(code, message string) ([]byte, error) {
	resp := model.PersonaResponse{
		Projects: []model.Project{},
		Error: &model.ErrorDetail{
			Code:    code,
			Message: message,
		},
	}
	return json.Marshal(resp)
}

// NewPersonaHandler creates a new MessageHandler with the given options.
func NewPersonaHandler(opts ...PersonaHandlerOption) port.MessageHandler {
	h := &personaHandler{
		cdpCache: cdp.NewCache(nil), // no-op cache by default
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Compile-time check.
var _ port.MessageHandler = (*personaHandler)(nil)
