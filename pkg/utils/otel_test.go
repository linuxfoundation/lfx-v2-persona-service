// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSetupOTelSDK guards against a semconv schema URL mismatch between our
// service resource and the SDK's resource.Default(), which makes setup fail
// with "conflicting Schema URL" after an SDK bump.
func TestSetupOTelSDK(t *testing.T) {
	t.Setenv("OTEL_TRACES_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")

	shutdown, err := SetupOTelSDK(context.Background())
	require.NoError(t, err)
	require.NoError(t, shutdown(context.Background()))
}
