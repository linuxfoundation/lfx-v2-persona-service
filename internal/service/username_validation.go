// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// isValidQueryUsername reports whether username is safe to embed in Query Service
// filter and tag clauses. LFX usernames are alphanumeric with underscores,
// hyphens, and periods; rejecting other characters prevents filter-syntax
// injection via commas, colons, wildcards, or quotes.
func isValidQueryUsername(username string) bool {
	if username == "" {
		return true
	}
	for _, r := range username {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-', r == '.':
			continue
		default:
			return false
		}
	}
	return true
}
