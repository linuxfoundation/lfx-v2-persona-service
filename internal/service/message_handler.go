// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/port"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/cdp"
)

// personaHandler implements port.MessageHandler.
type personaHandler struct {
	cdpClient *cdp.Client
	cdpCache  *cdp.Cache
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

	var projects []model.Project

	// Source 2: CDP roles and affiliations.
	if h.cdpClient != nil {
		cdpProjects, err := h.sourceCDPRoles(ctx, &req)
		if err != nil {
			// Partial failure — log and continue with other sources.
			slog.ErrorContext(ctx, "CDP source failed, skipping",
				"error", err,
			)
		} else {
			projects = model.MergeProjects(projects, cdpProjects)
		}
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

	// Step 3: Convert affiliations to detections.
	return affiliationsToProjects(affiliations)
}

func affiliationsToProjects(affiliations []cdp.ProjectAffiliation) ([]model.Project, error) {
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

		projects = append(projects, model.Project{
			ProjectUID:  aff.ID,
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
