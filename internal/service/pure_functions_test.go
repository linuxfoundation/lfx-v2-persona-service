// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"encoding/json"
	"testing"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/cdp"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// deduplicateResources
// ---------------------------------------------------------------------------

func TestDeduplicateResources_noArgs(t *testing.T) {
	result := deduplicateResources()
	assert.Nil(t, result)
}

func TestDeduplicateResources_emptySlice(t *testing.T) {
	result := deduplicateResources([]query.Resource{})
	assert.Nil(t, result)
}

func TestDeduplicateResources_singleSliceNoDuplicates(t *testing.T) {
	input := []query.Resource{
		{ID: "a"},
		{ID: "b"},
		{ID: "c"},
	}
	result := deduplicateResources(input)
	assert.Len(t, result, 3)
}

func TestDeduplicateResources_singleSliceWithDuplicates(t *testing.T) {
	input := []query.Resource{
		{ID: "a"},
		{ID: "b"},
		{ID: "a"},
	}
	result := deduplicateResources(input)
	assert.Len(t, result, 2)
	ids := []string{result[0].ID, result[1].ID}
	assert.ElementsMatch(t, []string{"a", "b"}, ids)
}

func TestDeduplicateResources_twoSlicesNoOverlap(t *testing.T) {
	s1 := []query.Resource{{ID: "a"}, {ID: "b"}}
	s2 := []query.Resource{{ID: "c"}, {ID: "d"}}
	result := deduplicateResources(s1, s2)
	assert.Len(t, result, 4)
}

func TestDeduplicateResources_twoSlicesWithOverlap(t *testing.T) {
	s1 := []query.Resource{{ID: "a"}, {ID: "b"}}
	s2 := []query.Resource{{ID: "b"}, {ID: "c"}}
	result := deduplicateResources(s1, s2)
	assert.Len(t, result, 3)
	// First-seen (from s1) wins — order should be a, b, c.
	assert.Equal(t, "a", result[0].ID)
	assert.Equal(t, "b", result[1].ID)
	assert.Equal(t, "c", result[2].ID)
}

// ---------------------------------------------------------------------------
// boardMemberDetections
// ---------------------------------------------------------------------------

func mustMarshalCommitteeMember(t *testing.T, d query.CommitteeMemberData) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(d)
	require.NoError(t, err)
	return b
}

func TestBoardMemberDetections_emptyInput(t *testing.T) {
	result := boardMemberDetections(nil)
	assert.Nil(t, result)
}

func TestBoardMemberDetections_skipsEmptyProjectUID(t *testing.T) {
	resources := []query.Resource{
		{
			ID:   "member-1",
			Data: mustMarshalCommitteeMember(t, query.CommitteeMemberData{
				ProjectUID: "", // should be skipped
			}),
		},
	}
	result := boardMemberDetections(resources)
	assert.Nil(t, result)
}

func TestBoardMemberDetections_skipsMalformedJSON(t *testing.T) {
	resources := []query.Resource{
		{ID: "bad", Data: json.RawMessage(`not-json`)},
	}
	result := boardMemberDetections(resources)
	assert.Nil(t, result)
}

func TestBoardMemberDetections_allFieldsMapped(t *testing.T) {
	resources := []query.Resource{
		{
			ID: "member-42",
			Data: mustMarshalCommitteeMember(t, query.CommitteeMemberData{
				ProjectUID:    "proj-001",
				ProjectSlug:   "test-project",
				CommitteeUID:  "comm-001",
				CommitteeName: "Test Committee",
				Role:          query.CommitteeMemberRole{Name: "Chair"},
				Voting:        query.CommitteeMemberVote{Status: "Voting"},
				Organization: query.CommitteeMemberOrg{
					ID:      "org-001",
					Name:    "Example Corp",
					Website: "https://example.com",
				},
			}),
		},
	}

	result := boardMemberDetections(resources)
	require.Len(t, result, 1)

	p := result[0]
	assert.Equal(t, "proj-001", p.ProjectUID)
	assert.Equal(t, "test-project", p.ProjectSlug)
	require.Len(t, p.Detections, 1)
	assert.Equal(t, model.SourceBoardMember, p.Detections[0].Source)

	var extra model.BoardMemberExtra
	require.NoError(t, json.Unmarshal(p.Detections[0].Extra, &extra))
	assert.Equal(t, "comm-001", extra.CommitteeUID)
	assert.Equal(t, "Test Committee", extra.CommitteeName)
	assert.Equal(t, "member-42", extra.CommitteeMemberUID) // r.ID, not data field
	assert.Equal(t, "Chair", extra.Role)
	assert.Equal(t, "Voting", extra.VotingStatus)
	assert.Equal(t, "org-001", extra.Organization.ID)
	assert.Equal(t, "Example Corp", extra.Organization.Name)
	assert.Equal(t, "https://example.com", extra.Organization.Website)
}

func TestBoardMemberDetections_multipleResources(t *testing.T) {
	resources := []query.Resource{
		{
			ID: "m1",
			Data: mustMarshalCommitteeMember(t, query.CommitteeMemberData{
				ProjectUID: "p1",
			}),
		},
		{
			ID: "m2",
			Data: mustMarshalCommitteeMember(t, query.CommitteeMemberData{
				ProjectUID: "p2",
			}),
		},
	}
	result := boardMemberDetections(resources)
	assert.Len(t, result, 2)
}

// ---------------------------------------------------------------------------
// committeeMemberDetections
// ---------------------------------------------------------------------------

func TestCommitteeMemberDetections_emptyInput(t *testing.T) {
	result := committeeMemberDetections(nil)
	assert.Nil(t, result)
}

func TestCommitteeMemberDetections_skipsEmptyProjectUID(t *testing.T) {
	resources := []query.Resource{
		{
			ID:   "m1",
			Data: mustMarshalCommitteeMember(t, query.CommitteeMemberData{ProjectUID: ""}),
		},
	}
	result := committeeMemberDetections(resources)
	assert.Nil(t, result)
}

func TestCommitteeMemberDetections_allFieldsMapped(t *testing.T) {
	resources := []query.Resource{
		{
			ID: "member-99",
			Data: mustMarshalCommitteeMember(t, query.CommitteeMemberData{
				ProjectUID:    "proj-002",
				ProjectSlug:   "another-project",
				CommitteeUID:  "comm-002",
				CommitteeName: "Governance Committee",
				Role:          query.CommitteeMemberRole{Name: "Member"},
			}),
		},
	}

	result := committeeMemberDetections(resources)
	require.Len(t, result, 1)

	p := result[0]
	assert.Equal(t, "proj-002", p.ProjectUID)
	assert.Equal(t, "another-project", p.ProjectSlug)
	require.Len(t, p.Detections, 1)
	assert.Equal(t, model.SourceCommitteeMember, p.Detections[0].Source)

	var extra model.CommitteeMemberExtra
	require.NoError(t, json.Unmarshal(p.Detections[0].Extra, &extra))
	assert.Equal(t, "comm-002", extra.CommitteeUID)
	assert.Equal(t, "Governance Committee", extra.CommitteeName)
	assert.Equal(t, "member-99", extra.CommitteeMemberUID) // r.ID
	assert.Equal(t, "Member", extra.Role)
}

func TestCommitteeMemberDetections_skipsMalformedJSON(t *testing.T) {
	resources := []query.Resource{
		{ID: "bad", Data: json.RawMessage(`{invalid}`)},
	}
	result := committeeMemberDetections(resources)
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// affiliationsToProjects
// ---------------------------------------------------------------------------

func strPtr(s string) *string { return &s }

func TestAffiliationsToProjects_emptyInput(t *testing.T) {
	result, err := affiliationsToProjects(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAffiliationsToProjects_skipsNonLFSlug(t *testing.T) {
	affiliations := []cdp.ProjectAffiliation{
		{ProjectSlug: "nonlf_some-external-project"},
	}
	slugMap := map[string]string{"nonlf_some-external-project": "uid-1"}
	result, err := affiliationsToProjects(affiliations, slugMap)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAffiliationsToProjects_skipsSlugNotInMap(t *testing.T) {
	affiliations := []cdp.ProjectAffiliation{
		{ProjectSlug: "my-project"},
	}
	result, err := affiliationsToProjects(affiliations, map[string]string{})
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAffiliationsToProjects_skipsEmptyUIDInMap(t *testing.T) {
	affiliations := []cdp.ProjectAffiliation{
		{ProjectSlug: "my-project"},
	}
	slugMap := map[string]string{"my-project": ""}
	result, err := affiliationsToProjects(affiliations, slugMap)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestAffiliationsToProjects_allFieldsMapped(t *testing.T) {
	endDate := "2025-12-31"
	affiliations := []cdp.ProjectAffiliation{
		{
			ProjectSlug:       "linux-kernel",
			ContributionCount: 42,
			Roles: []cdp.ProjectAffiliationRole{
				{
					ID:          "role-1",
					Role:        "contributor",
					StartDate:   "2024-01-01",
					EndDate:     nil,
					RepoURL:     "https://github.com/torvalds/linux",
					RepoFileURL: "https://github.com/torvalds/linux/blob/main/MAINTAINERS",
				},
				{
					ID:        "role-2",
					Role:      "reviewer",
					StartDate: "2024-06-01",
					EndDate:   &endDate,
				},
			},
		},
	}
	slugMap := map[string]string{"linux-kernel": "proj-kernel-uid"}

	result, err := affiliationsToProjects(affiliations, slugMap)
	require.NoError(t, err)
	require.Len(t, result, 1)

	p := result[0]
	assert.Equal(t, "proj-kernel-uid", p.ProjectUID)
	assert.Equal(t, "linux-kernel", p.ProjectSlug)
	require.Len(t, p.Detections, 1)
	assert.Equal(t, model.SourceCDPRoles, p.Detections[0].Source)

	var extra model.CDPRolesExtra
	require.NoError(t, json.Unmarshal(p.Detections[0].Extra, &extra))
	assert.Equal(t, 42, extra.ContributionCount)
	require.Len(t, extra.Roles, 2)
	assert.Equal(t, "role-1", extra.Roles[0].ID)
	assert.Equal(t, "contributor", extra.Roles[0].Role)
	assert.Equal(t, "2024-01-01", extra.Roles[0].StartDate)
	assert.Nil(t, extra.Roles[0].EndDate)
	assert.Equal(t, "https://github.com/torvalds/linux", extra.Roles[0].RepoURL)
	assert.Equal(t, "https://github.com/torvalds/linux/blob/main/MAINTAINERS", extra.Roles[0].RepoFileURL)
	assert.Equal(t, "reviewer", extra.Roles[1].Role)
	require.NotNil(t, extra.Roles[1].EndDate)
	assert.Equal(t, "2025-12-31", *extra.Roles[1].EndDate)
}

func TestAffiliationsToProjects_multipleAffiliations(t *testing.T) {
	affiliations := []cdp.ProjectAffiliation{
		{ProjectSlug: "proj-a"},
		{ProjectSlug: "proj-b"},
		{ProjectSlug: "nonlf_skip"},
		{ProjectSlug: "proj-c"},
	}
	slugMap := map[string]string{
		"proj-a": "uid-a",
		"proj-b": "uid-b",
		"proj-c": "uid-c",
	}

	result, err := affiliationsToProjects(affiliations, slugMap)
	require.NoError(t, err)
	assert.Len(t, result, 3) // nonlf_ skipped
	assert.Equal(t, "uid-a", result[0].ProjectUID)
	assert.Equal(t, "uid-b", result[1].ProjectUID)
	assert.Equal(t, "uid-c", result[2].ProjectUID)
}

// ---------------------------------------------------------------------------
// projectContainsWriter
// ---------------------------------------------------------------------------

func mustMarshalProjectData(t *testing.T, d query.ProjectData) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(d)
	require.NoError(t, err)
	return b
}

func TestProjectContainsWriter_malformedJSON(t *testing.T) {
	assert.False(t, projectContainsWriter(json.RawMessage(`not-json`), "alice"))
}

func TestProjectContainsWriter_emptyWriters(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{UID: "p1", Writers: nil})
	assert.False(t, projectContainsWriter(raw, "alice"))
}

func TestProjectContainsWriter_usernameNotPresent(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID: "p1",
		Writers: []query.ProjectPerson{
			{Username: "bob"},
			{Username: "charlie"},
		},
	})
	assert.False(t, projectContainsWriter(raw, "alice"))
}

func TestProjectContainsWriter_exactMatch(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID:     "p1",
		Writers: []query.ProjectPerson{{Username: "alice"}},
	})
	assert.True(t, projectContainsWriter(raw, "alice"))
}

func TestProjectContainsWriter_caseInsensitiveMatch(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID:     "p1",
		Writers: []query.ProjectPerson{{Username: "Alice"}},
	})
	assert.True(t, projectContainsWriter(raw, "alice"))
}

// ---------------------------------------------------------------------------
// projectContainsAuditor
// ---------------------------------------------------------------------------

func TestProjectContainsAuditor_malformedJSON(t *testing.T) {
	assert.False(t, projectContainsAuditor(json.RawMessage(`not-json`), "alice"))
}

func TestProjectContainsAuditor_emptyAuditors(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{UID: "p1", Auditors: nil})
	assert.False(t, projectContainsAuditor(raw, "alice"))
}

func TestProjectContainsAuditor_usernameNotPresent(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID: "p1",
		Auditors: []query.ProjectPerson{
			{Username: "bob"},
		},
	})
	assert.False(t, projectContainsAuditor(raw, "alice"))
}

func TestProjectContainsAuditor_exactMatch(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID:      "p1",
		Auditors: []query.ProjectPerson{{Username: "alice"}},
	})
	assert.True(t, projectContainsAuditor(raw, "alice"))
}

func TestProjectContainsAuditor_caseInsensitiveMatch(t *testing.T) {
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID:      "p1",
		Auditors: []query.ProjectPerson{{Username: "ALICE"}},
	})
	assert.True(t, projectContainsAuditor(raw, "alice"))
}

func TestProjectContainsWriter_doesNotMatchAuditors(t *testing.T) {
	// A username in the auditors list must NOT satisfy projectContainsWriter.
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID:      "p1",
		Auditors: []query.ProjectPerson{{Username: "alice"}},
		Writers:  nil,
	})
	assert.False(t, projectContainsWriter(raw, "alice"))
}

func TestProjectContainsAuditor_doesNotMatchWriters(t *testing.T) {
	// A username in the writers list must NOT satisfy projectContainsAuditor.
	raw := mustMarshalProjectData(t, query.ProjectData{
		UID:     "p1",
		Writers: []query.ProjectPerson{{Username: "alice"}},
	})
	assert.False(t, projectContainsAuditor(raw, "alice"))
}
