// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidQueryEmail(t *testing.T) {
	tests := []struct {
		email string
		want  bool
	}{
		{email: "alice@contractor.example.org", want: true},
		{email: "carol+lfx@example.com", want: true},
		{email: "alice.lfid@example.com", want: true},
		{email: "user_123@example.com", want: true},
		{email: "first%last@example.com", want: true},
		{email: "Alice@Contractor.Example.Org", want: true},
		{email: "", want: false},
		{email: "foo,bar@example.com", want: false},
		{email: "foo:bar@example.com", want: false},
		{email: "foo bar@example.com", want: false},
		{email: "foo\tbar@example.com", want: false},
		{email: `foo"bar@example.com`, want: false},
		{email: "foo*bar@example.com", want: false},
		{email: "foo'bar@example.com", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			assert.Equal(t, tt.want, isValidQueryEmail(tt.email))
		})
	}
}

func TestGetPersona_rejectsInvalidEmail(t *testing.T) {
	h := NewPersonaHandler(WithQueryService(queryTestClient(t, httpNotFoundHandler), nil))

	body, err := h.GetPersona(context.Background(), &staticMessenger{
		data: []byte(`{"username":"alice","email":"alice@example.com,foo:bar"}`),
	})
	require.NoError(t, err)

	var resp model.PersonaResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.NotNil(t, resp.Error)
	assert.Equal(t, "validation_error", resp.Error.Code)
	assert.Equal(t, []model.Project{}, resp.Projects)
}
