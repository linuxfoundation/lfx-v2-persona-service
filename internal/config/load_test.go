// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// envOrDefault
// ---------------------------------------------------------------------------

func TestEnvOrDefault_returnsFallbackWhenUnset(t *testing.T) {
	const key = "TEST_ENVORDEFAULT_UNSET_KEY"
	got := envOrDefault(key, "fallback-value")
	assert.Equal(t, "fallback-value", got)
}

func TestEnvOrDefault_returnsEnvValueWhenSet(t *testing.T) {
	const key = "TEST_ENVORDEFAULT_SET_KEY"
	t.Setenv(key, "from-env")
	got := envOrDefault(key, "fallback-value")
	assert.Equal(t, "from-env", got)
}

func TestEnvOrDefault_returnsFallbackWhenEmptyString(t *testing.T) {
	const key = "TEST_ENVORDEFAULT_EMPTY_KEY"
	t.Setenv(key, "")
	got := envOrDefault(key, "fallback-value")
	assert.Equal(t, "fallback-value", got)
}

// ---------------------------------------------------------------------------
// Load — CDP capability group
// ---------------------------------------------------------------------------

// allCDPVars lists the five environment variables that must all be present
// for CDP to be enabled.
var allCDPVars = map[string]string{
	"AUTH0_ISSUER_BASE_URL":       "https://auth0.example.com/",
	"AUTH0_CLIENT_ID":             "test-client-id",
	"AUTH0_M2M_PRIVATE_BASE64_KEY": "dGVzdC1rZXk=",
	"CDP_AUDIENCE":                "https://cdp.example.com/",
	"CDP_BASE_URL":                "https://cdp.example.com",
}

func setEnvVars(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoad_CDPDisabledByDefault(t *testing.T) {
	// No CDP vars set → CDPEnabled must be false.
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPEnabledWhenAllVarsPresent(t *testing.T) {
	setEnvVars(t, allCDPVars)
	cfg := Load()
	assert.True(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenAuth0IssuerMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, "AUTH0_ISSUER_BASE_URL")
	setEnvVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenClientIDMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, "AUTH0_CLIENT_ID")
	setEnvVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenPrivateKeyMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, "AUTH0_M2M_PRIVATE_BASE64_KEY")
	setEnvVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenCDPAudienceMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, "CDP_AUDIENCE")
	setEnvVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenCDPBaseURLMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, "CDP_BASE_URL")
	setEnvVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

// ---------------------------------------------------------------------------
// Load — NATS URL default
// ---------------------------------------------------------------------------

func TestLoad_NATSURLDefaultsToLocalhost(t *testing.T) {
	cfg := Load()
	assert.Equal(t, "nats://localhost:4222", cfg.NATSURL)
}

func TestLoad_NATSURLFromEnv(t *testing.T) {
	t.Setenv("NATS_URL", "nats://custom-host:4222")
	cfg := Load()
	assert.Equal(t, "nats://custom-host:4222", cfg.NATSURL)
}

// ---------------------------------------------------------------------------
// Load — HandlerTimeout default
// ---------------------------------------------------------------------------

func TestLoad_HandlerTimeoutDefaultsFourSeconds(t *testing.T) {
	cfg := Load()
	assert.Equal(t, 4*time.Second, cfg.HandlerTimeout)
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
