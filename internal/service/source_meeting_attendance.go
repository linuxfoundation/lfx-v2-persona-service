// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
)

// sourceMeetingAttendance queries the Query Service for v1_past_meeting_participant
// records using dual-leg tag lookups (email + username). Both legs use tags_all
// so no local post-filter is needed. De-duplicates by Resource.id and returns
// meeting_attendance detections with project info from the enriched record.
func (h *personaHandler) sourceMeetingAttendance(ctx context.Context, req *model.PersonaRequest, sub string) ([]model.Project, error) {
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
			Type:    "v1_past_meeting_participant",
			TagsAll: []string{"email:" + req.Email},
		})
		emailCh <- legResult{resources, err}
	}()

	// Username leg — skipped when sub is empty. Both email and username are
	// indexed as tags on these records, so tags_all is used for both legs.
	if sub != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resources, err := h.queryClient.Search(ctx, query.SearchParams{
				Type:    "v1_past_meeting_participant",
				TagsAll: []string{"username:" + sub},
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
		slog.ErrorContext(ctx, "meeting attendance email leg failed", "error", emailResult.err)
	}
	if usernameResult.err != nil {
		slog.ErrorContext(ctx, "meeting attendance username leg failed", "error", usernameResult.err)
	}

	if emailResult.err != nil && usernameResult.err != nil {
		return nil, emailResult.err
	}

	// De-duplicate by Resource.id. No post-filter needed — both legs use
	// tag lookups which are exact matches.
	seen := make(map[string]bool)
	var merged []query.Resource

	for _, r := range emailResult.resources {
		if !seen[r.ID] {
			seen[r.ID] = true
			merged = append(merged, r)
		}
	}

	for _, r := range usernameResult.resources {
		if !seen[r.ID] {
			seen[r.ID] = true
			merged = append(merged, r)
		}
	}

	slog.DebugContext(ctx, "meeting attendance queries returned",
		"email_count", len(emailResult.resources),
		"username_count", len(usernameResult.resources),
		"deduped_total", len(merged),
	)

	// Convert to projects, reading project info directly from enriched records.
	var projects []model.Project
	for _, r := range merged {
		var data query.MeetingParticipantData
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
				{Source: model.SourceMeetingAttendance},
			},
		})
	}

	return projects, nil
}
