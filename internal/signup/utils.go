package signup

import "strings"

// normalizeEmail lowercases and trims whitespace from an email address.
// Used across the signup package for consistent email handling.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
