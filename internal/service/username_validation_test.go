// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidQueryUsername(t *testing.T) {
	tests := []struct {
		username string
		want     bool
	}{
		{username: "", want: true},
		{username: "alice-lfid", want: true},
		{username: "user_123", want: true},
		{username: "foo,bar", want: false},
		{username: "foo:bar", want: false},
		{username: "foo*bar", want: false},
		{username: `foo"bar`, want: false},
		{username: "auth0|alice", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidQueryUsername(tt.username))
		})
	}
}

func TestGetPersona_rejectsInvalidUsername(t *testing.T) {
	h := NewPersonaHandler(WithQueryService(queryTestClient(t, httpNotFoundHandler), nil))

	body, err := h.GetPersona(context.Background(), &staticMessenger{
		data: []byte(`{"username":"foo,bar:*","email":"alice@example.com"}`),
	})
	require.NoError(t, err)

	var resp model.PersonaResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, "validation_error", resp.Error.Code)
	assert.Equal(t, []model.Project{}, resp.Projects)
}

func httpNotFoundHandler(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "unexpected query", http.StatusNotFound)
}
