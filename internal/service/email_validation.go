// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

// isValidQueryEmail reports whether email is safe to embed in Query Service
// filter clauses. Emails enter comma-joined `filters=` clauses, so anything
// outside a conservative charset (alphanumerics plus `.@_+%-`) is rejected to
// prevent filter-syntax injection via commas, colons, wildcards, or quotes.
// The charset covers the dot-atom addresses LF accounts use; an
// exotic-but-real address fails loudly as a validation_error rather than
// silently downgrading the requester to contributor.
//
// Unlike isValidQueryUsername, an empty string is rejected: email is required,
// so there is no valid empty case. GetPersona checks for an empty email first
// and reports it separately, but the guard is kept here so the helper stays
// safe for any caller that validates without that check.
func isValidQueryEmail(email string) bool {
	if email == "" {
		return false
	}
	for _, r := range email {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '@', r == '_', r == '%', r == '+', r == '-':
			continue
		default:
			return false
		}
	}
	return true
}
