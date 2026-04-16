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

// sourceBoardMemberAndCommittee runs two parallel branches:
//  1. Board-only committee_member query → board_member detections
//  2. All committee_member query (no category filter) → committee_member detections (Source 4)
//
// Both share the same username→sub resolution.
func (h *personaHandler) sourceBoardMemberAndCommittee(ctx context.Context, req *model.PersonaRequest, sub string) ([]model.Project, error) {
	// Run Board-only and All-committee queries in parallel.
	type branchResult struct {
		resources []query.Resource
		err       error
	}

	var wg sync.WaitGroup
	boardCh := make(chan branchResult, 1)
	allCh := make(chan branchResult, 1)

	wg.Add(2)
	go func() {
		defer wg.Done()
		r, err := h.queryCommitteeMembers(ctx, req, sub, true)
		boardCh <- branchResult{r, err}
	}()
	go func() {
		defer wg.Done()
		r, err := h.queryCommitteeMembers(ctx, req, sub, false)
		allCh <- branchResult{r, err}
	}()

	wg.Wait()

	boardResult := <-boardCh
	allResult := <-allCh

	if boardResult.err != nil {
		slog.ErrorContext(ctx, "board member query failed", "error", boardResult.err)
	}
	if allResult.err != nil {
		slog.ErrorContext(ctx, "all committee member query failed", "error", allResult.err)
	}

	// Collect all unique members across both branches.
	allMembers := deduplicateResources(boardResult.resources, allResult.resources)

	slog.DebugContext(ctx, "committee member queries returned",
		"board_count", len(boardResult.resources),
		"all_count", len(allResult.resources),
		"deduped_total", len(allMembers),
	)

	if len(allMembers) == 0 {
		return nil, nil
	}

	// Board Member detections from Board-only results.
	bmProjects := boardMemberDetections(boardResult.resources)
	// Source 4 detections from ALL committee results.
	cmProjects := committeeMemberDetections(allMembers)

	return model.MergeProjects(bmProjects, cmProjects), nil
}

// queryCommitteeMembers runs the dual-leg (email + username) query for
// committee_member resources. When boardOnly is true, the query is filtered
// to committee_category:Board; otherwise all categories are included.
func (h *personaHandler) queryCommitteeMembers(ctx context.Context, req *model.PersonaRequest, sub string, boardOnly bool) ([]query.Resource, error) {
	type legResult struct {
		resources []query.Resource
		err       error
	}

	var wg sync.WaitGroup
	emailCh := make(chan legResult, 1)
	usernameCh := make(chan legResult, 1)

	// Build tags for the email leg.
	emailTags := []string{"email:" + req.Email}
	if boardOnly {
		emailTags = append(emailTags, "committee_category:Board")
	}

	// Email leg — always runs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		resources, err := h.queryClient.Search(ctx, query.SearchParams{
			Type:    "committee_member",
			TagsAll: emailTags,
		})
		emailCh <- legResult{resources, err}
	}()

	// Username leg — skipped when sub is empty.
	// NOTE: The Query Service committee_member "username" field contains the
	// Auth0 sub (e.g. "auth0|abc123"), not the LFX username. The sub is
	// resolved once by the caller. This indirection can be removed once the
	// Query Service indexes the actual username.
	if sub != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			usernameTags := []string{}
			if boardOnly {
				usernameTags = append(usernameTags, "committee_category:Board")
			}
			params := query.SearchParams{
				Type:    "committee_member",
				Filters: []string{"username:" + sub},
			}
			if len(usernameTags) > 0 {
				params.TagsAll = usernameTags
			}
			resources, err := h.queryClient.Search(ctx, params)
			usernameCh <- legResult{resources, err}
		}()
	} else {
		usernameCh <- legResult{}
	}

	wg.Wait()

	emailResult := <-emailCh
	usernameResult := <-usernameCh

	label := "all committee"
	if boardOnly {
		label = "board member"
	}

	if emailResult.err != nil {
		slog.ErrorContext(ctx, label+" email leg failed", "error", emailResult.err)
	}
	if usernameResult.err != nil {
		slog.ErrorContext(ctx, label+" username leg failed", "error", usernameResult.err)
	}

	if emailResult.err != nil && usernameResult.err != nil {
		return nil, emailResult.err
	}

	// De-duplicate by Resource.id, with local exact post-filter on username
	// leg results.
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
		var data query.CommitteeMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		if !strings.EqualFold(data.Username, sub) {
			continue
		}
		seen[r.ID] = true
		merged = append(merged, r)
	}

	return merged, nil
}

// deduplicateResources merges resources from multiple slices, de-duplicating
// by Resource.ID.
func deduplicateResources(slices ...[]query.Resource) []query.Resource {
	seen := make(map[string]bool)
	var out []query.Resource
	for _, s := range slices {
		for _, r := range s {
			if !seen[r.ID] {
				seen[r.ID] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// boardMemberDetections converts committee_member resources into board_member
// detections, reading project info directly from the enriched record.
func boardMemberDetections(resources []query.Resource) []model.Project {
	var projects []model.Project

	for _, r := range resources {
		var data query.CommitteeMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		if data.ProjectUID == "" {
			continue
		}

		extra := model.BoardMemberExtra{
			CommitteeUID:       data.CommitteeUID,
			CommitteeName:      data.CommitteeName,
			CommitteeMemberUID: r.ID,
			Role:               data.Role.Name,
			VotingStatus:       data.Voting.Status,
			Organization: model.BoardMemberOrganization{
				ID:      data.Organization.ID,
				Name:    data.Organization.Name,
				Website: data.Organization.Website,
			},
		}
		extraJSON, err := json.Marshal(extra)
		if err != nil {
			continue
		}

		projects = append(projects, model.Project{
			ProjectUID:  data.ProjectUID,
			ProjectSlug: data.ProjectSlug,
			Detections: []model.Detection{
				{Source: model.SourceBoardMember, Extra: extraJSON},
			},
		})
	}

	return projects
}

// committeeMemberDetections converts committee_member resources into Source 4
// committee_member detections, reading project info directly from the enriched record.
func committeeMemberDetections(resources []query.Resource) []model.Project {
	var projects []model.Project

	for _, r := range resources {
		var data query.CommitteeMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}
		if data.ProjectUID == "" {
			continue
		}

		extra := model.CommitteeMemberExtra{
			CommitteeUID:       data.CommitteeUID,
			CommitteeName:      data.CommitteeName,
			CommitteeMemberUID: r.ID,
			Role:               data.Role.Name,
		}
		extraJSON, err := json.Marshal(extra)
		if err != nil {
			continue
		}

		projects = append(projects, model.Project{
			ProjectUID:  data.ProjectUID,
			ProjectSlug: data.ProjectSlug,
			Detections: []model.Detection{
				{Source: model.SourceCommitteeMember, Extra: extraJSON},
			},
		})
	}

	return projects
}
