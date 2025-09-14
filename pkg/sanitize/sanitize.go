package sanitize

import "regexp"

/* ======================= Regex Definitions ======================= */

// reEmail matches common email addresses (case-insensitive).
var reEmail = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`)

// rePhone matches common phone numbers (e.g., +xx..., 08xx..., (xxx) xxx-xxxx).
// Allowed characters: digits, spaces, dashes, dots, parentheses, plus.
// Requires at least 9 digits total to reduce false positives.
var rePhone = regexp.MustCompile(`\+?\d[\d\s\-\.$begin:math:text$$end:math:text$]{7,}\d`)

/* ======================= Public Functions ======================== */

// RedactPII replaces emails and phone numbers in a string with "[redacted ...]".
func RedactPII(s string) string {
	if s == "" {
		return s
	}
	s = reEmail.ReplaceAllString(s, "[redacted email]")
	s = rePhone.ReplaceAllString(s, "[redacted phone]")
	return s
}

// Summary truncates a string to max characters and appends "…".
// It tries to cut at the nearest space before the limit to avoid breaking words.
func Summary(s string, max int) string {
	if len(s) <= max {
		return s
	}
	i := max
	// Walk backward until a space is found
	for i > 0 && i < len(s) && s[i] != ' ' {
		i--
	}
	if i <= 0 {
		i = max
	}
	return s[:i] + "…"
}
