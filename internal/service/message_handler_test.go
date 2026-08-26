// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/query"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetPersona_handlerTimeoutCancelsSlowSources verifies that WithHandlerTimeout
// causes GetPersona to return before the deadline rather than hanging indefinitely
// when an upstream source is blocked.
func TestGetPersona_handlerTimeoutCancelsSlowSources(t *testing.T) {
	// Slow handler that blocks until its request context is cancelled.
	slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
	})

	srv := httptest.NewServer(slowHandler)
	t.Cleanup(srv.Close)

	client := query.NewClient(query.ClientConfig{BaseURL: srv.URL})
	h := NewPersonaHandler(
		WithQueryService(client, nil),
		WithHandlerTimeout(100*time.Millisecond),
	)

	msg := &staticMessenger{
		data: []byte(`{"username":"alice","email":"alice@example.com"}`),
	}

	start := time.Now()
	body, err := h.GetPersona(context.Background(), msg)
	elapsed := time.Since(start)

	require.NoError(t, err)
	// Must return well within 1s despite the source blocking indefinitely.
	assert.Less(t, elapsed, time.Second, "GetPersona should return promptly when handler timeout fires")

	var resp model.PersonaResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	// Timeout should surface as an explicit error so the caller can distinguish
	// a timed-out response from a genuine "no affiliations" result.
	require.NotNil(t, resp.Error)
	assert.Equal(t, "handler_timeout", resp.Error.Code)
	assert.Equal(t, []model.Project{}, resp.Projects)
}

// TestGetPersona_timeoutPreservesPartialResults verifies that sources completing
// before the deadline have their results included in the handler_timeout response,
// exercising the buffered-channel drain and timeoutResponse partial-results contract.
func TestGetPersona_timeoutPreservesPartialResults(t *testing.T) {
	boardMemberData, err := json.Marshal(query.CommitteeMemberData{
		ProjectUID:    "proj-abc",
		ProjectSlug:   "test-project",
		CommitteeUID:  "committee-1",
		CommitteeName: "Test Committee",
	})
	require.NoError(t, err)

	// committee_member queries return immediately with a known project;
	// all other source types (project_settings, groupsio_member, etc.) block.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "committee_member") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(query.ResourceResponse{
				Resources: []query.Resource{
					{ID: "member-1", Type: "committee_member", Data: boardMemberData},
				},
			})
			return
		}
		<-r.Context().Done()
		http.Error(w, "cancelled", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	client := query.NewClient(query.ClientConfig{BaseURL: srv.URL})
	h := NewPersonaHandler(
		WithQueryService(client, nil),
		WithHandlerTimeout(100*time.Millisecond),
	)

	body, err := h.GetPersona(context.Background(), &staticMessenger{
		data: []byte(`{"username":"alice","email":"alice@example.com"}`),
	})
	require.NoError(t, err)

	var resp model.PersonaResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, "handler_timeout", resp.Error.Code)
	// Sources that completed before the deadline must be in the partial response.
	require.NotEmpty(t, resp.Projects, "partial results from fast sources must survive into timeout response")
	assert.Equal(t, "proj-abc", resp.Projects[0].ProjectUID)
}

// TestGetPersona_noTimeoutWhenZero verifies that a zero handlerTimeout leaves the
// context untouched (no deadline is imposed).
func TestGetPersona_noTimeoutWhenZero(t *testing.T) {
	fastHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(query.ResourceResponse{Resources: nil})
	})

	srv := httptest.NewServer(fastHandler)
	t.Cleanup(srv.Close)

	client := query.NewClient(query.ClientConfig{BaseURL: srv.URL})
	// Zero timeout — no deadline should be set.
	h := NewPersonaHandler(WithQueryService(client, nil))

	body, err := h.GetPersona(context.Background(), &staticMessenger{
		data: []byte(`{"username":"alice","email":"alice@example.com"}`),
	})
	require.NoError(t, err)

	var resp model.PersonaResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	assert.Equal(t, []model.Project{}, resp.Projects)
	assert.Nil(t, resp.Error)
}
