package pgpmime

import (
	"mime"
	"strings"
	"testing"
)

func TestProtectedHeaders(t *testing.T) {
	entity := []byte("Content-Type: multipart/mixed;\r\n boundary=abc\r\n\r\n--abc\r\nContent-Type: text/plain\r\n\r\nAhoj\r\n--abc--\r\n")
	subj := mime.QEncoding.Encode("utf-8", "Tajná schůzka")
	out := ProtectHeaders(entity, [][2]string{{"Subject", subj}, {"From", "jan@example.org"}, {"Cc", ""}})
	s := string(out)
	if !strings.Contains(s, `protected-headers=v1`) || !strings.Contains(s, "boundary=abc") || strings.Contains(s, "Cc:") {
		t.Fatalf("header not rewritten:\n%s", s)
	}
	if !strings.HasSuffix(s, "\r\n\r\n--abc\r\nContent-Type: text/plain\r\n\r\nAhoj\r\n--abc--\r\n") {
		t.Fatalf("body changed:\n%s", s)
	}
	if got := ProtectedSubject(out); got != "Tajná schůzka" {
		t.Errorf("ProtectedSubject = %q", got)
	}
	if got := ProtectedSubject(entity); got != "" {
		t.Errorf("an unprotected entity has no protected subject, got %q", got)
	}
}
