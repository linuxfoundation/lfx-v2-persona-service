// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

const (
	// ServiceName is the name of the persona service.
	ServiceName = "lfx-v2-persona-service"
)

// Environment variable keys for required configuration.
const (
	NATSURLEnvKey         = "NATS_URL"
	QueryServiceURLEnvKey = "QUERY_SERVICE_URL"
)

// Environment variable keys for LFX API gateway (alternative to QUERY_SERVICE_URL).
const (
	LFXBaseURLEnvKey  = "LFX_BASE_URL"
	LFXAudienceEnvKey = "LFX_AUDIENCE"
)

// Environment variable keys for CDP (optional group).
const (
	Auth0IssuerBaseURLEnvKey       = "AUTH0_ISSUER_BASE_URL"
	Auth0ClientIDEnvKey            = "AUTH0_CLIENT_ID"
	Auth0M2MPrivateBase64KeyEnvKey = "AUTH0_M2M_PRIVATE_BASE64_KEY"
	CDPAudienceEnvKey              = "CDP_AUDIENCE"
	CDPBaseURLEnvKey               = "CDP_BASE_URL"
)

