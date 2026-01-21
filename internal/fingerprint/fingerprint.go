package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

type V1Input struct {
	Repo         string
	Framework    string
	TestName     string
	ErrorSigNorm string
	Platform     string
}

func V1(in V1Input) string {
	h := sha256.Sum256([]byte(in.Repo + "|" + in.TestName + "|" + in.ErrorSigNorm + "|" + in.Framework + "|" + in.Platform))
	return hex.EncodeToString(h[:])
}






func NormalizeErrorSignature(s string) string {
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.TrimSpace(s)
	replacements := []*regexp.Regexp{
		regexp.MustCompile(`0x[0-9a-fA-F]+`),
		regexp.MustCompile(`:\d+`),
		regexp.MustCompile(`\b\d+(?:\.\d+)?(ms|s|m|h)\b`),
		regexp.MustCompile(`\b\d{4,}\b`),
		regexp.MustCompile(`\b[0-9a-f]{7,40}\b`),
	}
	for _, re := range replacements {
		s = re.ReplaceAllString(s, "X")
	}
	space := regexp.MustCompile(`\s+`)
	s = space.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}