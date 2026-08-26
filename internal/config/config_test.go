// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseDurationEnv(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		fallback time.Duration
		want     time.Duration
	}{
		{
			name:     "unset uses fallback",
			envValue: "",
			fallback: 4 * time.Second,
			want:     4 * time.Second,
		},
		{
			name:     "valid duration is parsed",
			envValue: "3s",
			fallback: 4 * time.Second,
			want:     3 * time.Second,
		},
		{
			name:     "valid millisecond duration is parsed",
			envValue: "500ms",
			fallback: 4 * time.Second,
			want:     500 * time.Millisecond,
		},
		{
			name:     "invalid value uses fallback",
			envValue: "notaduration",
			fallback: 4 * time.Second,
			want:     4 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const key = "TEST_PARSE_DURATION_ENV_KEY"
			if tt.envValue != "" {
				t.Setenv(key, tt.envValue)
			}
			got := parseDurationEnv(key, tt.fallback)
			assert.Equal(t, tt.want, got)
		})
	}
}
