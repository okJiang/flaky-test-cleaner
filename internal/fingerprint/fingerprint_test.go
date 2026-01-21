package fingerprint

import (
	"strings"
	"testing"
)

func mustContain(t string, sub string) bool {
	return strings.Contains(t, sub)
}

func TestNormalizeErrorSignature(t *testing.T) {
	input := "panic: boom at file.go:1234 after 1.2s addr 0xabc123"

	norm := NormalizeErrorSignature(input)
	if mustContain(norm, "1234") {
		t.Fatalf("expected line numbers removed, got %q", norm)
	}
	if mustContain(norm, "1.2s") {
		t.Fatalf("expected durations removed, got %q", norm)
	}
	if mustContain(norm, "0xabc") {
		t.Fatalf("expected hex removed, got %q", norm)
	}
}
