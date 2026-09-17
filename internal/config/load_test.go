// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package config

import (
	"testing"
	"time"

	"github.com/linuxfoundation/lfx-v2-persona-service/pkg/constants"
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
	constants.Auth0IssuerBaseURLEnvKey:       "https://auth0.example.com/",
	constants.Auth0ClientIDEnvKey:            "test-client-id",
	constants.Auth0M2MPrivateBase64KeyEnvKey: "dGVzdC1rZXk=",
	constants.CDPAudienceEnvKey:              "https://cdp.example.com/",
	constants.CDPBaseURLEnvKey:               "https://cdp.example.com",
}

// setCDPVars clears every CDP env key first, then sets the provided subset.
// Clearing first ensures tests are isolated even when CDP credentials are
// exported in the developer's shell (e.g. via `source .env`).
func setCDPVars(t *testing.T, vars map[string]string) {
	t.Helper()
	for k := range allCDPVars {
		t.Setenv(k, "")
	}
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

func TestLoad_CDPDisabledByDefault(t *testing.T) {
	// Clear all CDP vars to ensure isolation from developer shell env.
	setCDPVars(t, nil)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPEnabledWhenAllVarsPresent(t *testing.T) {
	setCDPVars(t, allCDPVars)
	cfg := Load()
	assert.True(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenAuth0IssuerMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, constants.Auth0IssuerBaseURLEnvKey)
	setCDPVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenClientIDMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, constants.Auth0ClientIDEnvKey)
	setCDPVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenPrivateKeyMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, constants.Auth0M2MPrivateBase64KeyEnvKey)
	setCDPVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenCDPAudienceMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, constants.CDPAudienceEnvKey)
	setCDPVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

func TestLoad_CDPDisabledWhenCDPBaseURLMissing(t *testing.T) {
	vars := copyMap(allCDPVars)
	delete(vars, constants.CDPBaseURLEnvKey)
	setCDPVars(t, vars)
	cfg := Load()
	assert.False(t, cfg.CDPEnabled)
}

// ---------------------------------------------------------------------------
// Load — NATS URL default
// ---------------------------------------------------------------------------

func TestLoad_NATSURLDefaultsToLocalhost(t *testing.T) {
	t.Setenv(constants.NATSURLEnvKey, "") // isolate from developer shell env
	cfg := Load()
	assert.Equal(t, "nats://localhost:4222", cfg.NATSURL)
}

func TestLoad_NATSURLFromEnv(t *testing.T) {
	t.Setenv(constants.NATSURLEnvKey, "nats://custom-host:4222")
	cfg := Load()
	assert.Equal(t, "nats://custom-host:4222", cfg.NATSURL)
}

// ---------------------------------------------------------------------------
// Load — HandlerTimeout default
// ---------------------------------------------------------------------------

func TestLoad_HandlerTimeoutDefaultsFourSeconds(t *testing.T) {
	t.Setenv(constants.HandlerTimeoutEnvKey, "") // isolate from developer shell env
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
