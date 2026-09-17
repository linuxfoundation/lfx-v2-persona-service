// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// [I1] sourceWriterAuditor — gaps in existing coverage
// ---------------------------------------------------------------------------

// TestSourceWriterAuditor_sameProjectBothRoles verifies that when a resource
// appears in both the writers and auditors legs, the merged result contains a
// single Project entry carrying BOTH detection tokens (writer + auditor).
func TestSourceWriterAuditor_sameProjectBothRoles(t *testing.T) {
	projectData := json.RawMessage(`{
		"uid": "proj-dual",
		"writers":  [{"username": "carol-lfid"}],
		"auditors": [{"username": "carol-lfid"}]
	}`)

	// Both writer and auditor legs return the same resource ID.
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|writers.username:carol-lfid|": {
			{ID: "settings-1", Data: projectData},
		},
		"project_settings|auditors.username:carol-lfid|": {
			{ID: "settings-1", Data: projectData},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1, "same project must not be duplicated")

	p := projects[0]
	assert.Equal(t, "proj-dual", p.ProjectUID)

	sources := make([]string, 0, len(p.Detections))
	for _, d := range p.Detections {
		sources = append(sources, d.Source)
	}
	assert.Contains(t, sources, model.SourceWriter, "writer detection must be present")
	assert.Contains(t, sources, model.SourceAuditor, "auditor detection must be present")
}

// TestSourceWriterAuditor_postFilterRejectsResourceMissingUsername verifies
// that a resource returned by the query service but whose data.writers array
// does not contain the requested username is silently dropped.
func TestSourceWriterAuditor_postFilterRejectsResourceMissingUsername(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		// Server sends a resource whose writers array contains a different user.
		"project_settings|writers.username:carol-lfid|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "proj-wrong",
					"writers":  [{"username": "different-user"}],
					"auditors": []
				}`),
			},
		},
		"project_settings|auditors.username:carol-lfid|": nil,
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	assert.Empty(t, projects, "resource with mismatched username must be filtered out")
}

// TestSourceWriterAuditor_bothLegsFail_returnsError verifies that when both the
// writers and auditors HTTP legs return errors the function surfaces an error
// rather than returning partial or empty results silently.
func TestSourceWriterAuditor_bothLegsFail_returnsError(t *testing.T) {
	// Responding with 503 causes the query client to return an error.
	errorHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	})
	client := queryTestClient(t, errorHandler)
	h := &personaHandler{queryClient: client}

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.Error(t, err, "both legs failing must surface an error")
	assert.Nil(t, projects)
}

// ---------------------------------------------------------------------------
// [I2] queryCommitteeMembers — boardOnly=false and deduplication
// ---------------------------------------------------------------------------

// TestQueryCommitteeMembers_boardOnlyFalse_noTagFilter verifies that when
// boardOnly is false neither leg sends committee_category:Board in its tags.
func TestQueryCommitteeMembers_boardOnlyFalse_noTagFilter(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		// email leg: type=committee_member, no filters, tagsAll=email only
		"committee_member||email:alice@example.com": {
			{
				ID: "member-email",
				Data: json.RawMessage(`{
					"username":       "alice-lfid",
					"project_uid":    "proj-1",
					"project_slug":   "proj-1",
					"committee_uid":  "comm-1",
					"committee_name": "TAC",
					"role":           {"name": "Member"},
					"voting":         {"status": "Voting Rep"},
					"organization":   {}
				}`),
			},
		},
		// username leg: type=committee_member, filters=username, no tagsAll
		"committee_member|username:alice-lfid|": {
			{
				ID: "member-username",
				Data: json.RawMessage(`{
					"username":       "alice-lfid",
					"project_uid":    "proj-2",
					"project_slug":   "proj-2",
					"committee_uid":  "comm-2",
					"committee_name": "STC",
					"role":           {"name": "Chair"},
					"voting":         {"status": "Voting Rep"},
					"organization":   {}
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	resources, err := h.queryCommitteeMembers(context.Background(), &model.PersonaRequest{
		Username: "alice-lfid",
		Email:    "alice@example.com",
	}, false /* boardOnly=false */)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	// Verify no request carried the Board category tag.
	for _, req := range capture.requestsSnapshot() {
		assert.NotContains(t, req.tagsAll, "committee_category:Board",
			"boardOnly=false must not send Board category tag")
	}
}

// TestQueryCommitteeMembers_deduplicatesSameIDFromBothLegs verifies that when
// the email leg and the username leg both return a resource with the same ID
// the merged output contains that resource exactly once.
func TestQueryCommitteeMembers_deduplicatesSameIDFromBothLegs(t *testing.T) {
	sharedResource := query.Resource{
		ID: "member-shared",
		Data: json.RawMessage(`{
			"username":       "alice-lfid",
			"project_uid":    "proj-shared",
			"project_slug":   "proj-shared",
			"committee_uid":  "comm-1",
			"committee_name": "TAC",
			"role":           {"name": "Member"},
			"voting":         {"status": "Voting Rep"},
			"organization":   {}
		}`),
	}

	capture := newQueryRequestCapture(map[string][]query.Resource{
		// Both legs return the same member-shared ID.
		"committee_member||email:alice@example.com": {sharedResource},
		"committee_member|username:alice-lfid|":     {sharedResource},
	})
	h := testHandlerWithQuery(t, capture)

	resources, err := h.queryCommitteeMembers(context.Background(), &model.PersonaRequest{
		Username: "alice-lfid",
		Email:    "alice@example.com",
	}, false /* boardOnly=false */)
	require.NoError(t, err)
	require.Len(t, resources, 1, "duplicate resource ID from both legs must appear exactly once")
	assert.Equal(t, "member-shared", resources[0].ID)
}
