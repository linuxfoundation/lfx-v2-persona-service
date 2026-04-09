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

// sourceWriterAuditor queries the Query Service for project resources where
// the user's Auth0 sub appears in data.writers or data.auditors. Two parallel
// filter legs are issued, de-duplicated by Resource.id, with a local exact
// post-filter on the username field within each person entry.
func (h *personaHandler) sourceWriterAuditor(ctx context.Context, req *model.PersonaRequest, sub string) ([]model.Project, error) {
	if sub == "" {
		return nil, nil
	}

	type legResult struct {
		resources []query.Resource
		err       error
	}

	var wg sync.WaitGroup
	writersCh := make(chan legResult, 1)
	auditorsCh := make(chan legResult, 1)

	// Writers leg.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resources, err := h.queryClient.Search(ctx, query.SearchParams{
			Type:    "project_settings",
			Filters: []string{"writers.username:" + sub},
		})
		writersCh <- legResult{resources, err}
	}()

	// Auditors leg.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resources, err := h.queryClient.Search(ctx, query.SearchParams{
			Type:    "project_settings",
			Filters: []string{"auditors.username:" + sub},
		})
		auditorsCh <- legResult{resources, err}
	}()

	wg.Wait()

	writersResult := <-writersCh
	auditorsResult := <-auditorsCh

	if writersResult.err != nil {
		slog.ErrorContext(ctx, "writer/auditor writers leg failed", "error", writersResult.err)
	}
	if auditorsResult.err != nil {
		slog.ErrorContext(ctx, "writer/auditor auditors leg failed", "error", auditorsResult.err)
	}

	if writersResult.err != nil && auditorsResult.err != nil {
		return nil, writersResult.err
	}

	// De-duplicate by Resource.id with local exact post-filter.
	seen := make(map[string]bool)
	var merged []query.Resource

	for _, r := range writersResult.resources {
		if seen[r.ID] {
			continue
		}
		if !projectContainsUser(r.Data, sub) {
			continue
		}
		seen[r.ID] = true
		merged = append(merged, r)
	}

	for _, r := range auditorsResult.resources {
		if seen[r.ID] {
			continue
		}
		if !projectContainsUser(r.Data, sub) {
			continue
		}
		seen[r.ID] = true
		merged = append(merged, r)
	}

	slog.DebugContext(ctx, "writer/auditor queries returned",
		"writers_count", len(writersResult.resources),
		"auditors_count", len(auditorsResult.resources),
		"deduped_total", len(merged),
	)

	var projects []model.Project
	for _, r := range merged {
		var data query.ProjectData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		if data.UID == "" {
			continue
		}

		slug := h.resolveProjectSlug(ctx, data.UID)

		projects = append(projects, model.Project{
			ProjectUID:  data.UID,
			ProjectSlug: slug,
			Detections: []model.Detection{
				{Source: model.SourceWriterAuditor},
			},
		})
	}

	return projects, nil
}

// projectContainsUser checks whether sub appears as a username in the
// project's writers or auditors arrays (case-insensitive).
func projectContainsUser(raw json.RawMessage, sub string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, w := range data.Writers {
		if strings.EqualFold(w.Username, sub) {
			return true
		}
	}
	for _, a := range data.Auditors {
		if strings.EqualFold(a.Username, sub) {
			return true
		}
	}
	return false
}
