package ui

import (
	"testing"
	"time"
)

func TestSessionHold(t *testing.T) {
	s := &session{}
	s.hold("a")
	s.hold("b")
	if h := s.held(); !h["a"] || !h["b"] || len(h) != 2 {
		t.Fatalf("held = %v", h)
	}
	s.release("a")
	if h := s.held(); h["a"] || !h["b"] {
		t.Fatalf("after release = %v", h)
	}
	// A check that never finishes does not hide the message for ever.
	s.mu.Lock()
	s.checked["b"] = time.Now().Add(-maxHold - time.Second)
	s.mu.Unlock()
	if h := s.held(); len(h) != 0 {
		t.Fatalf("expired = %v", h)
	}
}
