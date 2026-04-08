// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
)

// sourceExecutiveDirector queries the Query Service for project_settings
// resources where data.executive_director.username matches the user's Auth0
// sub, applies a local exact post-filter, resolves the project slug, and
// returns executive_director detections (no extra).
func (h *personaHandler) sourceExecutiveDirector(ctx context.Context, req *model.PersonaRequest, sub string) ([]model.Project, error) {
	if sub == "" {
		return nil, nil
	}

	resources, err := h.queryClient.Search(ctx, query.SearchParams{
		Type:    "project_settings",
		Filters: []string{"executive_director.username:" + sub},
	})
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "executive director query returned", "count", len(resources))

	var projects []model.Project
	for _, r := range resources {
		var data edSettingsData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		// Local post-filter: exact match on the nested username field.
		if !strings.EqualFold(data.ExecutiveDirector.Username, sub) {
			continue
		}

		projectUID := data.UID
		if projectUID == "" {
			continue
		}

		// Resolve slug from the project resource.
		slug := h.resolveProjectSlug(ctx, projectUID)

		projects = append(projects, model.Project{
			ProjectUID:  projectUID,
			ProjectSlug: slug,
			Detections: []model.Detection{
				{Source: model.SourceExecutiveDirector},
			},
		})
	}

	return projects, nil
}

const projectServiceGetSlug = "lfx.projects-api.get_slug"

// resolveProjectSlug looks up a project slug by UID via the project service NATS endpoint.
func (h *personaHandler) resolveProjectSlug(ctx context.Context, projectUID string) string {
	if h.natsClient == nil {
		return ""
	}
	resp, err := h.natsClient.Request(ctx, projectServiceGetSlug, []byte(projectUID))
	if err != nil {
		slog.WarnContext(ctx, "project uid→slug resolution failed", "uid", projectUID, "error", err)
		return ""
	}
	slug := strings.TrimSpace(string(resp))
	if slug == "" || (len(resp) > 0 && resp[0] == '{') {
		return ""
	}
	return slug
}

// edSettingsData extracts the fields needed for ED detection from a project_settings resource.
type edSettingsData struct {
	UID               string        `json:"uid"`
	ExecutiveDirector edNestedField `json:"executive_director"`
}

type edNestedField struct {
	Username string `json:"username"`
}
