package macbridge_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Imbecile6197/klient/internal/macbridge"
)

// Ensures loginStart is not stuck behind resumeAll (mutex deadlock regression).
func TestLoginConcurrentWithResume(t *testing.T) {
	macbridge.Init(func(string) {})
	done := make(chan string, 2)
	go func() {
		t0 := time.Now()
		r := macbridge.Call(`{"method":"accounts.resumeAll","id":1}`)
		done <- "resumeAll " + time.Since(t0).Round(time.Millisecond).String() + " " + clipLogin(r, 120)
	}()
	time.Sleep(50 * time.Millisecond)
	go func() {
		t0 := time.Now()
		req, _ := json.Marshal(map[string]any{
			"method": "proton.loginStart", "id": 2,
			"params": map[string]string{"username": "probe-not-real", "password": "wrong-pass-probe"},
		})
		r := macbridge.Call(string(req))
		done <- "loginStart " + time.Since(t0).Round(time.Millisecond).String() + " " + clipLogin(r, 240)
	}()

	timeout := time.After(45 * time.Second)
	var loginLine string
	for i := 0; i < 2; i++ {
		select {
		case line := <-done:
			t.Log(line)
			if strings.HasPrefix(line, "loginStart") {
				loginLine = line
			}
		case <-timeout:
			t.Fatal("timed out — likely Engine.mu deadlock in addSession/startEvents")
		}
	}
	if loginLine == "" {
		t.Fatal("loginStart did not finish")
	}
	// Wrong credentials should return quickly with an API/error JSON, not hang.
	if !strings.Contains(loginLine, `"ok"`) && !strings.Contains(loginLine, "error") {
		t.Fatalf("unexpected login response: %s", loginLine)
	}
}

func clipLogin(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
