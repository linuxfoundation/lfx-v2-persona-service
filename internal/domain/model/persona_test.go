// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeProjects_emptyBoth(t *testing.T) {
	result := MergeProjects(nil, nil)
	assert.Empty(t, result)
}

func TestMergeProjects_emptyDst(t *testing.T) {
	src := []Project{
		{ProjectUID: "p1", ProjectSlug: "slug-1", Detections: []Detection{{Source: SourceBoardMember}}},
	}
	result := MergeProjects(nil, src)
	assert.Equal(t, src, result)
}

func TestMergeProjects_emptySrc(t *testing.T) {
	dst := []Project{
		{ProjectUID: "p1", ProjectSlug: "slug-1", Detections: []Detection{{Source: SourceBoardMember}}},
	}
	result := MergeProjects(dst, nil)
	assert.Equal(t, dst, result)
}

func TestMergeProjects_disjointUIDs(t *testing.T) {
	dst := []Project{
		{ProjectUID: "p1", ProjectSlug: "slug-1", Detections: []Detection{{Source: SourceBoardMember}}},
	}
	src := []Project{
		{ProjectUID: "p2", ProjectSlug: "slug-2", Detections: []Detection{{Source: SourceCommitteeMember}}},
	}

	result := MergeProjects(dst, src)

	assert.Len(t, result, 2)
	assert.Equal(t, "p1", result[0].ProjectUID)
	assert.Equal(t, "p2", result[1].ProjectUID)
}

func TestMergeProjects_sameUID_appendsDetections(t *testing.T) {
	dst := []Project{
		{ProjectUID: "p1", ProjectSlug: "slug-1", Detections: []Detection{{Source: SourceBoardMember}}},
	}
	src := []Project{
		{ProjectUID: "p1", ProjectSlug: "slug-1", Detections: []Detection{{Source: SourceCommitteeMember}}},
	}

	result := MergeProjects(dst, src)

	assert.Len(t, result, 1)
	assert.Len(t, result[0].Detections, 2)
	assert.Equal(t, SourceBoardMember, result[0].Detections[0].Source)
	assert.Equal(t, SourceCommitteeMember, result[0].Detections[1].Source)
}

func TestMergeProjects_orderPreservation(t *testing.T) {
	// dst entries must stay before newly-added src entries.
	dst := []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceBoardMember}}},
		{ProjectUID: "p2", Detections: []Detection{{Source: SourceCDPRoles}}},
	}
	src := []Project{
		{ProjectUID: "p3", Detections: []Detection{{Source: SourceMailingList}}},
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceCommitteeMember}}},
	}

	result := MergeProjects(dst, src)

	assert.Len(t, result, 3)
	assert.Equal(t, "p1", result[0].ProjectUID)
	assert.Equal(t, "p2", result[1].ProjectUID)
	assert.Equal(t, "p3", result[2].ProjectUID)
	// p1 now has two detections from both calls.
	assert.Len(t, result[0].Detections, 2)
}

func TestMergeProjects_multipleCallsAccumulateDetections(t *testing.T) {
	// Merging three times with the same UID produces three detections.
	var projects []Project
	projects = MergeProjects(projects, []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceBoardMember}}},
	})
	projects = MergeProjects(projects, []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceCDPRoles}}},
	})
	projects = MergeProjects(projects, []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceMailingList}}},
	})

	assert.Len(t, projects, 1)
	assert.Len(t, projects[0].Detections, 3)
}

func TestMergeProjects_srcHasDuplicateUID(t *testing.T) {
	// If src itself contains the same UID twice, the second occurrence appends
	// to the dst entry created by the first.
	dst := []Project{}
	src := []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceBoardMember}}},
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceCDPRoles}}},
	}

	result := MergeProjects(dst, src)

	assert.Len(t, result, 1)
	assert.Len(t, result[0].Detections, 2)
}

func TestMergeProjects_dstHasDuplicateUID(t *testing.T) {
	// If dst itself contains the same UID twice (e.g. boardMemberDetections
	// emitting one entry per resource for the same project), MergeProjects must
	// normalise them into a single entry before applying src, so the response
	// preserves the one-project-with-multiple-detections contract.
	dst := []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceBoardMember}}},
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceBoardMember}}},
	}
	src := []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceCommitteeMember}}},
	}

	result := MergeProjects(dst, src)

	assert.Len(t, result, 1, "duplicate UIDs in dst must be collapsed into one project")
	assert.Len(t, result[0].Detections, 3, "all detections from both dst entries and src must be present")
}

func TestMergeProjects_dstHasDuplicateUID_noSrc(t *testing.T) {
	// Normalisation must apply even when src is empty.
	dst := []Project{
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceBoardMember}}},
		{ProjectUID: "p1", Detections: []Detection{{Source: SourceCDPRoles}}},
	}

	result := MergeProjects(dst, nil)

	assert.Len(t, result, 1)
	assert.Len(t, result[0].Detections, 2)
}
