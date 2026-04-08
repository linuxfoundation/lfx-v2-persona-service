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

// sourceMailingList queries the Query Service for groupsio_member records
// using the dual-leg (email + username) pattern, de-duplicates by Resource.id,
// and returns mailing_list detections with project info read directly from
// the enriched record.
func (h *personaHandler) sourceMailingList(ctx context.Context, req *model.PersonaRequest, sub string) ([]model.Project, error) {
	type legResult struct {
		resources []query.Resource
		err       error
	}

	var wg sync.WaitGroup
	emailCh := make(chan legResult, 1)
	usernameCh := make(chan legResult, 1)

	// Email leg — always runs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resources, err := h.queryClient.Search(ctx, query.SearchParams{
			Type:    "groupsio_member",
			TagsAll: []string{"email:" + req.Email},
		})
		emailCh <- legResult{resources, err}
	}()

	// Username leg — skipped when sub is empty.
	if sub != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resources, err := h.queryClient.Search(ctx, query.SearchParams{
				Type:    "groupsio_member",
				Filters: []string{"username:" + sub},
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
		slog.ErrorContext(ctx, "mailing list email leg failed", "error", emailResult.err)
	}
	if usernameResult.err != nil {
		slog.ErrorContext(ctx, "mailing list username leg failed", "error", usernameResult.err)
	}

	if emailResult.err != nil && usernameResult.err != nil {
		return nil, emailResult.err
	}

	// De-duplicate by Resource.id with local exact post-filter on username leg.
	seen := make(map[string]bool)
	var merged []query.Resource

	for _, r := range emailResult.resources {
		if !seen[r.ID] {
			seen[r.ID] = true
			merged = append(merged, r)
		}
	}

	for _, r := range usernameResult.resources {
		if seen[r.ID] {
			continue
		}
		var data query.GroupsIOMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		if !strings.EqualFold(data.Username, sub) {
			continue
		}
		seen[r.ID] = true
		merged = append(merged, r)
	}

	slog.DebugContext(ctx, "mailing list queries returned",
		"email_count", len(emailResult.resources),
		"username_count", len(usernameResult.resources),
		"deduped_total", len(merged),
	)

	// Convert to projects, reading project info directly from enriched records.
	var projects []model.Project
	for _, r := range merged {
		var data query.GroupsIOMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		if data.ProjectUID == "" {
			continue
		}

		projects = append(projects, model.Project{
			ProjectUID:  data.ProjectUID,
			ProjectSlug: data.ProjectSlug,
			Detections: []model.Detection{
				{Source: model.SourceMailingList},
			},
		})
	}

	return projects, nil
}
