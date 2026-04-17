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
// filter legs are issued. Each leg produces its own detection token (writer or
// auditor). A project where the user holds both roles receives both detections
// on a single project entry. A local exact post-filter is applied per-leg,
// checking only the relevant array, to guard against overly liberal term matches.
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

	// Track which source token(s) matched per Resource.ID.
	type projectMatch struct {
		resource query.Resource
		sources  []string
	}
	matches := make(map[string]*projectMatch)

	for _, r := range writersResult.resources {
		if !projectContainsWriter(r.Data, sub) {
			continue
		}
		if m, ok := matches[r.ID]; ok {
			m.sources = append(m.sources, model.SourceWriter)
		} else {
			matches[r.ID] = &projectMatch{resource: r, sources: []string{model.SourceWriter}}
		}
	}

	for _, r := range auditorsResult.resources {
		if !projectContainsAuditor(r.Data, sub) {
			continue
		}
		if m, ok := matches[r.ID]; ok {
			m.sources = append(m.sources, model.SourceAuditor)
		} else {
			matches[r.ID] = &projectMatch{resource: r, sources: []string{model.SourceAuditor}}
		}
	}

	slog.DebugContext(ctx, "writer/auditor queries returned",
		"writers_count", len(writersResult.resources),
		"auditors_count", len(auditorsResult.resources),
		"matched_projects", len(matches),
	)

	var projects []model.Project
	for _, m := range matches {
		var data query.ProjectData
		if err := json.Unmarshal(m.resource.Data, &data); err != nil {
			continue
		}
		if data.UID == "" {
			continue
		}

		slug := h.resolveProjectSlug(ctx, data.UID)

		detections := make([]model.Detection, 0, len(m.sources))
		for _, src := range m.sources {
			detections = append(detections, model.Detection{Source: src})
		}

		projects = append(projects, model.Project{
			ProjectUID:  data.UID,
			ProjectSlug: slug,
			Detections:  detections,
		})
	}

	return projects, nil
}

// projectContainsWriter checks whether sub appears as a username in the
// project's writers array (case-insensitive).
func projectContainsWriter(raw json.RawMessage, sub string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, w := range data.Writers {
		if strings.EqualFold(w.Username, sub) {
			return true
		}
	}
	return false
}

// projectContainsAuditor checks whether sub appears as a username in the
// project's auditors array (case-insensitive).
func projectContainsAuditor(raw json.RawMessage, sub string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, a := range data.Auditors {
		if strings.EqualFold(a.Username, sub) {
			return true
		}
	}
	return false
}
