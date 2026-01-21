package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
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
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	return s
}