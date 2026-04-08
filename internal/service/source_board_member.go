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

	// Collect all unique members across both branches for committee resolution.
	allMembers := deduplicateResources(boardResult.resources, allResult.resources)

	slog.DebugContext(ctx, "committee member queries returned",
		"board_count", len(boardResult.resources),
		"all_count", len(allResult.resources),
		"deduped_total", len(allMembers),
	)

	if len(allMembers) == 0 {
		return nil, nil
	}

	// Resolve committee UIDs → project info (shared by both detection types).
	committeeMap, err := h.resolveCommittees(ctx, allMembers)
	if err != nil {
		slog.WarnContext(ctx, "committee→project resolution partially failed", "error", err)
	}

	slog.DebugContext(ctx, "committee resolution done", "resolved", len(committeeMap))

	// Board Member detections from Board-only results.
	bmProjects := boardMemberDetections(boardResult.resources, committeeMap)
	// Source 4 detections from ALL committee results.
	cmProjects := committeeMemberDetections(allMembers, committeeMap)

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

// resolveCommittees fetches committee resources in parallel to resolve
// committee_uid → project info. Returns a map of committee_uid → CommitteeData.
func (h *personaHandler) resolveCommittees(ctx context.Context, members []query.Resource) (map[string]query.CommitteeData, error) {
	unique := make(map[string]bool)
	for _, r := range members {
		var data query.CommitteeMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			slog.WarnContext(ctx, "failed to unmarshal committee_member data for UID extraction",
				"id", r.ID,
				"error", err,
				"raw_data_len", len(r.Data),
			)
			continue
		}
		unique[data.CommitteeUID] = true
	}

	slog.DebugContext(ctx, "unique committee UIDs to resolve", "count", len(unique))

	type result struct {
		uid  string
		data query.CommitteeData
		err  error
	}

	var wg sync.WaitGroup
	ch := make(chan result, len(unique))

	for uid := range unique {
		wg.Add(1)
		go func(committeeUID string) {
			defer wg.Done()
			resources, err := h.queryClient.Search(ctx, query.SearchParams{
				Type:    "committee",
				TagsAll: []string{committeeUID},
			})
			if err != nil {
				ch <- result{uid: committeeUID, err: err}
				return
			}
			if len(resources) == 0 {
				ch <- result{uid: committeeUID, err: fmt.Errorf("committee %s not found", committeeUID)}
				return
			}
			var cd query.CommitteeData
			if err := json.Unmarshal(resources[0].Data, &cd); err != nil {
				ch <- result{uid: committeeUID, err: err}
				return
			}
			ch <- result{uid: committeeUID, data: cd}
		}(uid)
	}

	wg.Wait()
	close(ch)

	out := make(map[string]query.CommitteeData, len(unique))
	var firstErr error
	for r := range ch {
		if r.err != nil {
			slog.WarnContext(ctx, "failed to resolve committee", "committee_uid", r.uid, "error", r.err)
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		out[r.uid] = r.data
	}

	return out, firstErr
}

// boardMemberDetections converts committee_member resources into board_member
// detections, using committeeMap to resolve project info.
func boardMemberDetections(resources []query.Resource, committeeMap map[string]query.CommitteeData) []model.Project {
	var projects []model.Project

	for _, r := range resources {
		var data query.CommitteeMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}

		committee, ok := committeeMap[data.CommitteeUID]
		if !ok {
			continue
		}

		extra := model.BoardMemberExtra{
			CommitteeUID:       data.CommitteeUID,
			CommitteeName:      data.CommitteeName,
			CommitteeMemberUID: r.ID,
			Role:               data.Role.Name,
			VotingStatus:       data.Voting.Status,
		}
		extraJSON, err := json.Marshal(extra)
		if err != nil {
			continue
		}

		projects = append(projects, model.Project{
			ProjectUID:  committee.ProjectUID,
			ProjectSlug: committee.ProjectSlug,
			Detections: []model.Detection{
				{Source: model.SourceBoardMember, Extra: extraJSON},
			},
		})
	}

	return projects
}

// committeeMemberDetections converts committee_member resources into Source 4
// committee_member detections, using committeeMap to resolve project info.
func committeeMemberDetections(resources []query.Resource, committeeMap map[string]query.CommitteeData) []model.Project {
	var projects []model.Project

	for _, r := range resources {
		var data query.CommitteeMemberData
		if err := json.Unmarshal(r.Data, &data); err != nil {
			continue
		}

		committee, ok := committeeMap[data.CommitteeUID]
		if !ok {
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
			ProjectUID:  committee.ProjectUID,
			ProjectSlug: committee.ProjectSlug,
			Detections: []model.Detection{
				{Source: model.SourceCommitteeMember, Extra: extraJSON},
			},
		})
	}

	return projects
}
