// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package config

import (
	"log/slog"
	"os"

	"github.com/linuxfoundation/lfx-v2-persona-service/pkg/constants"
)

// Config holds the resolved service configuration.
type Config struct {
	NATSURL         string
	QueryServiceURL string

	// LFX API gateway (used when QUERY_SERVICE_URL is not set).
	LFXBaseURL  string
	LFXAudience string

	CDPEnabled bool

	// CDP credentials (populated only when CDPEnabled is true).
	Auth0IssuerBaseURL       string
	Auth0ClientID            string
	Auth0M2MPrivateBase64Key string
	CDPAudience              string
	CDPBaseURL               string
}

// Load reads configuration from environment variables and determines which
// optional capability groups are available. Missing optional groups are logged
// as warnings; the service continues with degraded functionality.
func Load() Config {
	cfg := Config{
		NATSURL:         envOrDefault(constants.NATSURLEnvKey, "nats://localhost:4222"),
		QueryServiceURL: os.Getenv(constants.QueryServiceURLEnvKey),
		LFXBaseURL:      os.Getenv(constants.LFXBaseURLEnvKey),
		LFXAudience:     os.Getenv(constants.LFXAudienceEnvKey),
	}

	if cfg.QueryServiceURL == "" && cfg.LFXBaseURL == "" {
		slog.Warn("neither QUERY_SERVICE_URL nor LFX_BASE_URL is set — Query Service sources will be disabled")
	}

	// CDP credential group — all five must be present to enable.
	cfg.Auth0IssuerBaseURL = os.Getenv(constants.Auth0IssuerBaseURLEnvKey)
	cfg.Auth0ClientID = os.Getenv(constants.Auth0ClientIDEnvKey)
	cfg.Auth0M2MPrivateBase64Key = os.Getenv(constants.Auth0M2MPrivateBase64KeyEnvKey)
	cfg.CDPAudience = os.Getenv(constants.CDPAudienceEnvKey)
	cfg.CDPBaseURL = os.Getenv(constants.CDPBaseURLEnvKey)

	cfg.CDPEnabled = cfg.Auth0IssuerBaseURL != "" &&
		cfg.Auth0ClientID != "" &&
		cfg.Auth0M2MPrivateBase64Key != "" &&
		cfg.CDPAudience != "" &&
		cfg.CDPBaseURL != ""

	if !cfg.CDPEnabled {
		slog.Warn("CDP credentials incomplete — sources cdp_activity and cdp_roles will be disabled",
			"hint", "set AUTH0_ISSUER_BASE_URL, AUTH0_CLIENT_ID, AUTH0_M2M_PRIVATE_BASE64_KEY, CDP_AUDIENCE, CDP_BASE_URL to enable",
		)
	} else {
		slog.Info("CDP capability enabled")
	}

	return cfg
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
