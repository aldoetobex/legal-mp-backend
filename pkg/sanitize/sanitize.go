package sanitize

import (
	"regexp"
	"unicode"
)

// Email sederhana (case-insensitive)
var reEmail = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

// Telepon: deretan karakter telepon umum (+, digit, spasi, dash, titik, kurung).
// Kita ganti hanya jika memiliki >=8 digit agar tidak over-match.
var rePhoneLike = regexp.MustCompile(`(?i)\+?[\d\-\s().]{8,}`)

// hitung minimal jumlah digit di sebuah string
func countDigits(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsDigit(r) {
			n++
		}
	}
	return n
}

func RedactPII(s string) string {
	if s == "" {
		return s
	}
	// Mask email
	s = reEmail.ReplaceAllString(s, "[redacted email]")

	// Mask nomor telepon hanya jika kandidat mengandung >=8 digit
	s = rePhoneLike.ReplaceAllStringFunc(s, func(m string) string {
		if countDigits(m) >= 8 {
			return "[redacted phone]"
		}
		return m
	})

	return s
}

// Summary memotong teks ke panjang max, di batas spasi bila mungkin.
func Summary(s string, max int) string {
	if max <= 0 || len(s) <= max {
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
