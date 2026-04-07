// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/domain/port"
)

// personaHandler implements port.MessageHandler.
// In this skeleton it returns an empty projects list for every valid request.
type personaHandler struct{}

// GetPersona validates the request and returns an empty persona response.
// Source implementations will be added in follow-up tickets.
func (h *personaHandler) GetPersona(ctx context.Context, msg port.TransportMessenger) ([]byte, error) {

	var req model.PersonaRequest
	if err := json.Unmarshal(msg.Data(), &req); err != nil {
		slog.ErrorContext(ctx, "failed to unmarshal persona request", "error", err)
		return errorResponse("invalid_request", "malformed JSON payload")
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Username = strings.TrimSpace(req.Username)

	if req.Email == "" {
		return errorResponse("validation_error", "email is required")
	}

	slog.DebugContext(ctx, "persona request received",
		"username", req.Username,
		"email", req.Email,
	)

	// Skeleton: return empty projects, no error.
	// Source fan-out will be added in follow-up tickets.
	resp := model.PersonaResponse{
		Projects: []model.Project{},
		Error:    nil,
	}

	return json.Marshal(resp)
}

// errorResponse builds a PersonaResponse with an error and empty projects.
func errorResponse(code, message string) ([]byte, error) {
	resp := model.PersonaResponse{
		Projects: []model.Project{},
		Error: &model.ErrorDetail{
			Code:    code,
			Message: message,
		},
	}
	return json.Marshal(resp)
}

// NewPersonaHandler creates a new MessageHandler.
func NewPersonaHandler() port.MessageHandler {
	return &personaHandler{}
}

// Compile-time check.
var _ port.MessageHandler = (*personaHandler)(nil)
