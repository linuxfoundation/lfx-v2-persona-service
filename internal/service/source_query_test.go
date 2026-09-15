// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type queryRequestCapture struct {
	mu        sync.Mutex
	requests  []queryRequestRecord
	responses map[string][]query.Resource
}

type queryRequestRecord struct {
	resourceType string
	tagsAll      string
	filters      string
}

func newQueryRequestCapture(responses map[string][]query.Resource) *queryRequestCapture {
	return &queryRequestCapture{responses: responses}
}

func (c *queryRequestCapture) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		record := queryRequestRecord{
			resourceType: q.Get("type"),
			tagsAll:      q.Get("tags_all"),
			filters:      q.Get("filters"),
		}
		c.mu.Lock()
		c.requests = append(c.requests, record)
		key := record.resourceType + "|" + record.filters + "|" + record.tagsAll
		resources := c.responses[key]
		c.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(query.ResourceResponse{Resources: resources})
	}
}

func (c *queryRequestCapture) requestsSnapshot() []queryRequestRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]queryRequestRecord, len(c.requests))
	copy(out, c.requests)
	return out
}

func queryTestClient(t *testing.T, handler http.HandlerFunc) *query.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return query.NewClient(query.ClientConfig{BaseURL: srv.URL})
}

func testHandlerWithQuery(t *testing.T, capture *queryRequestCapture) *personaHandler {
	t.Helper()
	return &personaHandler{queryClient: queryTestClient(t, capture.handler())}
}

type staticMessenger struct {
	data []byte
}

func (m *staticMessenger) Subject() string      { return "lfx.personas-api.get" }
func (m *staticMessenger) Data() []byte         { return m.data }
func (m *staticMessenger) Respond([]byte) error { return nil }

func TestSourceExecutiveDirector_emptyUsernameStillQueriesEmail(t *testing.T) {
	capture := newQueryRequestCapture(nil)
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)
	assert.Empty(t, projects)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 1)
	assert.Equal(t, "executive_director.email:alice@example.com", requests[0].filters)
	assert.NotContains(t, requests[0].filters, "executive_director.username:")
}

func TestSourceExecutiveDirector_usesUsernameDirectly(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|executive_director.username:carol-lfid|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"executive_director": {"username": "carol-lfid"}
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	assert.Equal(t, model.SourceExecutiveDirector, projects[0].Detections[0].Source)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 2)
	filters := []string{requests[0].filters, requests[1].filters}
	assert.Contains(t, filters, "executive_director.username:carol-lfid")
	assert.Contains(t, filters, "executive_director.email:carol@example.com")
	for _, r := range requests {
		assert.Equal(t, "project_settings", r.resourceType)
	}
}

func TestSourceExecutiveDirector_postFilterRejectsMismatchedUsername(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|executive_director.username:carol-lfid|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"executive_director": {"username": "other-user"}
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestSourceExecutiveDirector_emailLegMatches(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|executive_director.email:alice@example.com|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"executive_director": {"username": "alice-lfid", "email": "alice@example.com"}
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	assert.Equal(t, model.SourceExecutiveDirector, projects[0].Detections[0].Source)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 1)
	assert.Equal(t, "executive_director.email:alice@example.com", requests[0].filters)
}

func TestSourceExecutiveDirector_postFilterRejectsMismatchedEmail(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|executive_director.email:alice@example.com|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"executive_director": {"username": "alice-lfid", "email": "other@example.com"}
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestSourceExecutiveDirector_dualLegDeduplicates(t *testing.T) {
	resource := query.Resource{
		ID: "settings-1",
		Data: json.RawMessage(`{
			"uid": "project-1",
			"executive_director": {"username": "alice-lfid", "email": "alice@example.com"}
		}`),
	}
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|executive_director.email:alice@example.com|": {resource},
		"project_settings|executive_director.username:alice-lfid|":     {resource},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Username: "alice-lfid",
		Email:    "alice@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	require.Len(t, projects[0].Detections, 1)
	assert.Equal(t, model.SourceExecutiveDirector, projects[0].Detections[0].Source)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 2)
}

func TestSourceWriterAuditor_emptyUsernameStillQueriesEmail(t *testing.T) {
	capture := newQueryRequestCapture(nil)
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)
	assert.Empty(t, projects)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 2)
	filters := []string{requests[0].filters, requests[1].filters}
	assert.Contains(t, filters, "writers.email:alice@example.com")
	assert.Contains(t, filters, "auditors.email:alice@example.com")
	for _, r := range requests {
		assert.NotContains(t, r.filters, ".username:")
	}
}

func TestSourceWriterAuditor_usesUsernameDirectly(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|writers.username:carol-lfid|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"writers": [{"username": "carol-lfid"}],
					"auditors": []
				}`),
			},
		},
		"project_settings|auditors.username:carol-lfid|": {
			{
				ID: "settings-2",
				Data: json.RawMessage(`{
					"uid": "project-2",
					"writers": [],
					"auditors": [{"username": "CAROL-LFID"}]
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 2)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 4)
	filters := []string{requests[0].filters, requests[1].filters, requests[2].filters, requests[3].filters}
	assert.Contains(t, filters, "writers.username:carol-lfid")
	assert.Contains(t, filters, "auditors.username:carol-lfid")
	assert.Contains(t, filters, "writers.email:carol@example.com")
	assert.Contains(t, filters, "auditors.email:carol@example.com")
}

func TestSourceWriterAuditor_emailLegsMatch(t *testing.T) {
	resource := query.Resource{
		ID: "settings-1",
		Data: json.RawMessage(`{
			"uid": "project-1",
			"writers": [{"username": "alice-lfid", "email": "alice@example.com"}],
			"auditors": [{"username": "alice-lfid", "email": "alice@example.com"}]
		}`),
	}
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|writers.email:alice@example.com|":  {resource},
		"project_settings|auditors.email:alice@example.com|": {resource},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Email: "alice@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	require.Len(t, projects[0].Detections, 2)
	assert.Equal(t, model.SourceWriter, projects[0].Detections[0].Source)
	assert.Equal(t, model.SourceAuditor, projects[0].Detections[1].Source)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 2)
	filters := []string{requests[0].filters, requests[1].filters}
	assert.Contains(t, filters, "writers.email:alice@example.com")
	assert.Contains(t, filters, "auditors.email:alice@example.com")
}

func TestSourceWriterAuditor_mixedLegsSameProject(t *testing.T) {
	resource := query.Resource{
		ID: "settings-1",
		Data: json.RawMessage(`{
			"uid": "project-1",
			"writers": [{"username": "other-user", "email": "carol@example.com"}],
			"auditors": [{"username": "carol-lfid", "email": "other@example.com"}]
		}`),
	}
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|writers.email:carol@example.com|": {resource},
		"project_settings|auditors.username:carol-lfid|":    {resource},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	require.Len(t, projects[0].Detections, 2)
	assert.Equal(t, model.SourceWriter, projects[0].Detections[0].Source)
	assert.Equal(t, model.SourceAuditor, projects[0].Detections[1].Source)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 4)
}

func TestQueryCommitteeMembers_usernameLegUsesLFIDUsername(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"committee_member||committee_category:Board,email:alice@example.com": nil,
		"committee_member|username:carol-lfid|committee_category:Board": {
			{
				ID: "member-1",
				Data: json.RawMessage(`{
					"username": "CAROL-LFID",
					"project_uid": "project-1",
					"project_slug": "proj-1",
					"committee_uid": "committee-1",
					"committee_name": "TAC",
					"role": {"name": "Member"},
					"voting": {"status": "Voting Rep"},
					"organization": {"id": "org-1", "name": "LF"}
				}`),
			},
			{
				ID: "member-2",
				Data: json.RawMessage(`{
					"username": "other-user",
					"project_uid": "project-2",
					"project_slug": "proj-2",
					"committee_uid": "committee-2",
					"committee_name": "Other",
					"role": {"name": "Member"},
					"voting": {"status": "Voting Rep"},
					"organization": {"id": "org-2", "name": "Other"}
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	resources, err := h.queryCommitteeMembers(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "alice@example.com",
	}, true)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "member-1", resources[0].ID)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 2)
	assert.Contains(t, []string{requests[0].filters, requests[1].filters}, "username:carol-lfid")
}

func TestSourceMailingList_usernameLegUsesLFIDUsername(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"groupsio_member||email:alice@example.com": nil,
		"groupsio_member|username:carol-lfid|": {
			{
				ID: "member-1",
				Data: json.RawMessage(`{
					"username": "carol-lfid",
					"project_uid": "project-1",
					"project_slug": "proj-1"
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceMailingList(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "alice@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	assert.Equal(t, model.SourceMailingList, projects[0].Detections[0].Source)
}

func TestSourceMeetingAttendance_usernameLegUsesTagLookup(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"v1_past_meeting_participant||email:alice@example.com": nil,
		"v1_past_meeting_participant||username:carol-lfid": {
			{
				ID: "participant-1",
				Data: json.RawMessage(`{
					"username": "carol-lfid",
					"project_uid": "project-1",
					"project_slug": "proj-1"
				}`),
			},
		},
	})
	h := testHandlerWithQuery(t, capture)

	projects, err := h.sourceMeetingAttendance(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "alice@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)

	requests := capture.requestsSnapshot()
	require.Len(t, requests, 2)
	assert.Contains(t, []string{requests[0].tagsAll, requests[1].tagsAll}, "username:carol-lfid")
}

// queryFailureHandler makes every Query Service call fail, so a source's
// all-legs-failed branch can be exercised.
func queryFailureHandler(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "upstream unavailable", http.StatusInternalServerError)
}

func TestSourceExecutiveDirector_allLegsFailReturnsError(t *testing.T) {
	tests := []struct {
		name string
		req  model.PersonaRequest
	}{
		{
			name: "with username",
			req:  model.PersonaRequest{Username: "carol-lfid", Email: "carol@example.com"},
		},
		{
			// The email leg is the only leg here, so its failure is total and
			// must still surface rather than reading as "no ED role".
			name: "without username",
			req:  model.PersonaRequest{Email: "carol@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &personaHandler{queryClient: queryTestClient(t, queryFailureHandler)}

			projects, err := h.sourceExecutiveDirector(context.Background(), &tt.req)
			require.Error(t, err)
			assert.Empty(t, projects)
		})
	}
}

func TestSourceExecutiveDirector_singleLegFailureDegrades(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|executive_director.username:carol-lfid|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"executive_director": {"username": "carol-lfid", "email": "carol@example.com"}
				}`),
			},
		},
	})
	// Fail only the email leg; the username leg must still produce results.
	h := &personaHandler{queryClient: queryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("filters"), "executive_director.email:") {
			http.Error(w, "upstream unavailable", http.StatusInternalServerError)
			return
		}
		capture.handler()(w, r)
	})}

	projects, err := h.sourceExecutiveDirector(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
}

func TestSourceWriterAuditor_allLegsFailReturnsError(t *testing.T) {
	tests := []struct {
		name string
		req  model.PersonaRequest
	}{
		{
			name: "with username",
			req:  model.PersonaRequest{Username: "carol-lfid", Email: "carol@example.com"},
		},
		{
			// Only the two email legs run here, so the all-legs-failed check
			// must compare against the legs actually issued, not a fixed four.
			name: "without username",
			req:  model.PersonaRequest{Email: "carol@example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &personaHandler{queryClient: queryTestClient(t, queryFailureHandler)}

			projects, err := h.sourceWriterAuditor(context.Background(), &tt.req)
			require.Error(t, err)
			assert.Empty(t, projects)
		})
	}
}

func TestSourceWriterAuditor_partialLegFailureDegrades(t *testing.T) {
	capture := newQueryRequestCapture(map[string][]query.Resource{
		"project_settings|auditors.email:carol@example.com|": {
			{
				ID: "settings-1",
				Data: json.RawMessage(`{
					"uid": "project-1",
					"auditors": [{"username": "other-user", "email": "carol@example.com"}]
				}`),
			},
		},
	})
	// Fail every leg except the auditors email leg.
	h := &personaHandler{queryClient: queryTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filters") != "auditors.email:carol@example.com" {
			http.Error(w, "upstream unavailable", http.StatusInternalServerError)
			return
		}
		capture.handler()(w, r)
	})}

	projects, err := h.sourceWriterAuditor(context.Background(), &model.PersonaRequest{
		Username: "carol-lfid",
		Email:    "carol@example.com",
	})
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "project-1", projects[0].ProjectUID)
	require.Len(t, projects[0].Detections, 1)
	assert.Equal(t, model.SourceAuditor, projects[0].Detections[0].Source)
}
