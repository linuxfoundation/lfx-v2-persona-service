// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"

	personaservice "github.com/linuxfoundation/lfx-v2-persona-service/gen/persona_service"
	"github.com/linuxfoundation/lfx-v2-persona-service/internal/infrastructure/nats"
)

type personaSvc struct {
	natsClient *nats.NATSClient
}

// Livez implements the liveness check endpoint.
func (s *personaSvc) Livez(ctx context.Context) ([]byte, error) {
	return []byte("OK"), nil
}

// Readyz implements the readiness check endpoint.
func (s *personaSvc) Readyz(ctx context.Context) ([]byte, error) {
	if s.natsClient != nil {
		if err := s.natsClient.IsReady(ctx); err != nil {
			return nil, fmt.Errorf("NATS not ready: %w", err)
		}
	}

	return []byte("OK"), nil
}

// NewPersonaService creates a new persona health-check service.
func NewPersonaService() personaservice.Service {
	return &personaSvc{
		natsClient: getNATSClient(),
	}
}
