package macbridge

import (
	"encoding/json"
	"testing"
)

func TestEchoAndVersion(t *testing.T) {
	Init(nil)
	raw := Call(`{"method":"version","id":1}`)
	var res Response
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("version: %+v", res)
	}
	raw = Call(`{"method":"echo","id":2,"params":{"a":1}}`)
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("echo: %+v", res)
	}
}

func TestKeyringSmoke(t *testing.T) {
	Init(nil)
	raw := Call(`{"method":"keyring.smoke","id":3}`)
	var res Response
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("keyring.smoke: %+v (allow Klíčenka access if prompted)", res)
	}
}
