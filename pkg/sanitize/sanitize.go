package sanitize

import "regexp"

// Email biasa (case-insensitive)
var reEmail = regexp.MustCompile(`(?i)[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}`)

// Telepon yang umum: +xx..., (xxx) xxx-xxxx, 08xx..., dsb
// Catatan: karakter yang diizinkan hanya digit, spasi, tanda minus, titik, kurung, dan plus.
// Minimal 9 digit total agar tidak terlalu agresif.
var rePhone = regexp.MustCompile(`\+?\d[\d\s\-\.$begin:math:text$$end:math:text$]{7,}\d`)

func RedactPII(s string) string {
	if s == "" {
		return s
	}
	s = reEmail.ReplaceAllString(s, "[redacted email]")
	s = rePhone.ReplaceAllString(s, "[redacted phone]")
	return s
}

// Potong ringkasan untuk listing
func Summary(s string, max int) string {
	if len(s) <= max {
		return s
	}
	i := max
	for i > 0 && i < len(s) && s[i] != ' ' {
		i--
	}
	if i <= 0 {
		i = max
	}
	return s[:i] + "…"
}
