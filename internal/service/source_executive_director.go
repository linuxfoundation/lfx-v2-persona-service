// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
)

// sourceExecutiveDirector queries the Query Service for project_settings
// resources whose executive_director matches the caller — by email (always)
// and by username (when present) — applies a local exact post-filter per leg,
// resolves the project slug, and returns executive_director detections
// (no extra). The email leg exists because the login-session username does
// not always equal the LFID stored by the v1→v2 sync, while the stored ED
// email does match the session email.
func (h *personaHandler) sourceExecutiveDirector(ctx context.Context, req *model.PersonaRequest) ([]model.Project, error) {
	type legResult struct {
		resources []query.Resource
		err       error
	}

	var wg sync.WaitGroup
	emailCh := make(chan legResult, 1)
	usernameCh := make(chan legResult, 1)

	// Email leg — always runs. project_settings nests the ED email inside an
	// object, so it is only reachable via a dot-notation `filters` clause, not
	// the `email:` tag used by the per-person resource types. That clause is
	// case-sensitive, so this leg assumes the indexer stores the email
	// lowercased, matching the handler normalization in GetPersona.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resources, err := h.queryClient.Search(ctx, query.SearchParams{
			Type:    "project_settings",
			Filters: []string{"executive_director.email:" + req.Email},
		})
		emailCh <- legResult{resources, err}
	}()

	// Username leg — skipped when username is empty.
	usernameLegRan := req.Username != ""
	if usernameLegRan {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resources, err := h.queryClient.Search(ctx, query.SearchParams{
				Type:    "project_settings",
				Filters: []string{"executive_director.username:" + req.Username},
			})
			usernameCh <- legResult{resources, err}
		}()
	} else {
		usernameCh <- legResult{}
	}

	wg.Wait()

	emailResult := <-emailCh
	usernameResult := <-usernameCh

	if emailResult.err != nil {
		slog.ErrorContext(ctx, "executive director email leg failed", "error", emailResult.err)
	}
	if usernameResult.err != nil {
		slog.ErrorContext(ctx, "executive director username leg failed", "error", usernameResult.err)
	}
	// Surface an error only when every leg that actually ran failed — with no
	// username the email leg is the only leg, so its failure is total.
	if emailResult.err != nil && (!usernameLegRan || usernameResult.err != nil) {
		return nil, emailResult.err
	}

	slog.DebugContext(ctx, "executive director queries returned",
		"email_count", len(emailResult.resources),
		"username_count", len(usernameResult.resources),
	)

	// Merge the legs, de-duplicating by resource ID and post-filtering each
	// leg on its own identity field (exact, case-insensitive).
	seen := make(map[string]bool)
	var projects []model.Project
	add := func(resources []query.Resource, matches func(edNestedField) bool) {
		for _, r := range resources {
			if seen[r.ID] {
				continue
			}
			var data edSettingsData
			if err := json.Unmarshal(r.Data, &data); err != nil {
				continue
			}
			if !matches(data.ExecutiveDirector) {
				continue
			}
			if data.UID == "" {
				continue
			}
			seen[r.ID] = true
			projects = append(projects, model.Project{
				ProjectUID:  data.UID,
				ProjectSlug: h.resolveProjectSlug(ctx, data.UID),
				Detections: []model.Detection{
					{Source: model.SourceExecutiveDirector},
				},
			})
		}
	}

	add(emailResult.resources, func(ed edNestedField) bool {
		return strings.EqualFold(ed.Email, req.Email)
	})
	add(usernameResult.resources, func(ed edNestedField) bool {
		return strings.EqualFold(ed.Username, req.Username)
	})

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
	Email    string `json:"email"`
}
