// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	// Sources failed — projects should be empty (autodegrade, not an error response).
	assert.Equal(t, []model.Project{}, resp.Projects)
	assert.Nil(t, resp.Error)
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
