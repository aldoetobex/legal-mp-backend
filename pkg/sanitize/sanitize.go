// pkg/sanitize/sanitize.go
package sanitize

import "regexp"

var (
	reEmail = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`)
	// Deteksi nomor telp yang umum: +xx xxx..., (xxx) xxx-xxxx, 08xx..., dst
	rePhone = regexp.MustCompile(`\+?\d[\d\s\-$begin:math:text$$end:math:text$.]{7,}\d`)
)

func RedactPII(s string) string {
	if s == "" {
		return s
	}
	s = reEmail.ReplaceAllString(s, "[redacted email]")
	s = rePhone.ReplaceAllString(s, "[redacted phone]")
	return s
}

// Optional: potong ringkasan untuk listing
func Summary(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// potong di batas kata biar rapi
	i := max
	for i > 0 && i < len(s) && s[i] != ' ' {
		i--
	}
	if i <= 0 {
		i = max
	}
	return s[:i] + "…"
}
