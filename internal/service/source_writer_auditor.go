// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
)

// sourceWriterAuditor queries the Query Service for project_settings resources
// where the caller appears in data.writers or data.auditors. Four parallel
// filter legs are issued: username and email legs per role. The email legs
// exist because the login-session username does not always equal the LFID
// stored by the v1→v2 sync, while the stored email does match the session
// email. Each leg produces its own detection token (writer or auditor). A
// project where the user holds both roles receives both detections on a
// single project entry. A local exact post-filter is applied per-leg,
// checking only the relevant array against the leg's own identity field, to
// guard against overly liberal term matches.
func (h *personaHandler) sourceWriterAuditor(ctx context.Context, req *model.PersonaRequest) ([]model.Project, error) {
	type legResult struct {
		resources []query.Resource
		err       error
	}

	var wg sync.WaitGroup
	writersEmailCh := make(chan legResult, 1)
	writersUsernameCh := make(chan legResult, 1)
	auditorsEmailCh := make(chan legResult, 1)
	auditorsUsernameCh := make(chan legResult, 1)

	search := func(filter string, ch chan<- legResult) {
		defer wg.Done()
		resources, err := h.queryClient.Search(ctx, query.SearchParams{
			Type:    "project_settings",
			Filters: []string{filter},
		})
		ch <- legResult{resources, err}
	}

	// Email legs — always run; username legs are conditional, so track how many
	// legs were actually issued to decide when every leg has failed.
	//
	// writers/auditors are arrays of objects, so the emails are only reachable
	// via dot-notation `filters` clauses, not the `email:` tag used by the
	// per-person resource types. Those clauses are case-sensitive, so these legs
	// assume the indexer stores emails lowercased, matching the handler
	// normalization in GetPersona.
	legsRun := 2
	wg.Add(2)
	go search("writers.email:"+req.Email, writersEmailCh)
	go search("auditors.email:"+req.Email, auditorsEmailCh)

	// Username legs — skipped when username is empty.
	if req.Username != "" {
		legsRun = 4
		wg.Add(2)
		go search("writers.username:"+req.Username, writersUsernameCh)
		go search("auditors.username:"+req.Username, auditorsUsernameCh)
	} else {
		writersUsernameCh <- legResult{}
		auditorsUsernameCh <- legResult{}
	}

	wg.Wait()

	writersEmailResult := <-writersEmailCh
	writersUsernameResult := <-writersUsernameCh
	auditorsEmailResult := <-auditorsEmailCh
	auditorsUsernameResult := <-auditorsUsernameCh

	failures := 0
	var firstErr error
	logLegFailure := func(label string, err error) {
		if err == nil {
			return
		}
		failures++
		if firstErr == nil {
			firstErr = err
		}
		slog.ErrorContext(ctx, "writer/auditor "+label+" leg failed", "error", err)
	}
	logLegFailure("writers.email", writersEmailResult.err)
	logLegFailure("writers.username", writersUsernameResult.err)
	logLegFailure("auditors.email", auditorsEmailResult.err)
	logLegFailure("auditors.username", auditorsUsernameResult.err)

	if failures == legsRun {
		return nil, firstErr
	}

	// Track which source token(s) matched per Resource.ID.
	type projectMatch struct {
		resource query.Resource
		sources  []string
	}
	matches := make(map[string]*projectMatch)

	addLeg := func(resources []query.Resource, source string, contains func(json.RawMessage, string) bool, identity string) {
		for _, r := range resources {
			if !contains(r.Data, identity) {
				continue
			}
			if m, ok := matches[r.ID]; ok {
				if !slices.Contains(m.sources, source) {
					m.sources = append(m.sources, source)
				}
			} else {
				matches[r.ID] = &projectMatch{resource: r, sources: []string{source}}
			}
		}
	}

	addLeg(writersEmailResult.resources, model.SourceWriter, projectContainsWriterEmail, req.Email)
	addLeg(writersUsernameResult.resources, model.SourceWriter, projectContainsWriter, req.Username)
	addLeg(auditorsEmailResult.resources, model.SourceAuditor, projectContainsAuditorEmail, req.Email)
	addLeg(auditorsUsernameResult.resources, model.SourceAuditor, projectContainsAuditor, req.Username)

	slog.DebugContext(ctx, "writer/auditor queries returned",
		"writers_email_count", len(writersEmailResult.resources),
		"writers_username_count", len(writersUsernameResult.resources),
		"auditors_email_count", len(auditorsEmailResult.resources),
		"auditors_username_count", len(auditorsUsernameResult.resources),
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

// projectContainsWriter checks whether username appears in the project's
// writers array (case-insensitive).
func projectContainsWriter(raw json.RawMessage, username string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, w := range data.Writers {
		if strings.EqualFold(w.Username, username) {
			return true
		}
	}
	return false
}

// projectContainsWriterEmail checks whether email appears in the project's
// writers array (case-insensitive).
func projectContainsWriterEmail(raw json.RawMessage, email string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, w := range data.Writers {
		if strings.EqualFold(w.Email, email) {
			return true
		}
	}
	return false
}

// projectContainsAuditor checks whether username appears in the project's
// auditors array (case-insensitive).
func projectContainsAuditor(raw json.RawMessage, username string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, a := range data.Auditors {
		if strings.EqualFold(a.Username, username) {
			return true
		}
	}
	return false
}

// projectContainsAuditorEmail checks whether email appears in the project's
// auditors array (case-insensitive).
func projectContainsAuditorEmail(raw json.RawMessage, email string) bool {
	var data query.ProjectData
	if err := json.Unmarshal(raw, &data); err != nil {
		return false
	}
	for _, a := range data.Auditors {
		if strings.EqualFold(a.Email, email) {
			return true
		}
	}
	return false
}
